package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

func (r *WorkspaceRepository) KnowledgeItems(ctx context.Context, limit int, includeDeleted bool) ([]KnowledgeItem, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	query := `SELECT public_id FROM knowledge_items`
	if !includeDeleted {
		query += ` WHERE status <> 'deleted'`
	}
	query += ` ORDER BY updated_at DESC,id DESC LIMIT ?`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]KnowledgeItem, 0, len(ids))
	for _, id := range ids {
		item, loadErr := r.KnowledgeItem(ctx, id)
		if loadErr != nil {
			return nil, loadErr
		}
		item.Revisions = nil
		result = append(result, item)
	}
	return result, nil
}

func (r *WorkspaceRepository) KnowledgeItem(ctx context.Context, publicID string) (KnowledgeItem, error) {
	var item KnowledgeItem
	var internalID uint64
	var current uint64
	err := r.db.QueryRowContext(ctx, `SELECT id,public_id,title,status,current_revision_no,created_at,updated_at
FROM knowledge_items WHERE public_id=?`, strings.TrimSpace(publicID)).Scan(
		&internalID, &item.ID, &item.Title, &item.Status, &current, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return KnowledgeItem{}, ErrNotFound
	}
	if err != nil {
		return KnowledgeItem{}, err
	}
	rows, err := r.db.QueryContext(ctx, knowledgeRevisionSelect+`
WHERE revision.knowledge_item_id=? ORDER BY revision.revision_no DESC`, internalID)
	if err != nil {
		return KnowledgeItem{}, err
	}
	defer func() { _ = rows.Close() }()
	item.Revisions = make([]KnowledgeRevision, 0)
	for rows.Next() {
		revision, scanErr := scanKnowledgeRevision(rows)
		if scanErr != nil {
			return KnowledgeItem{}, scanErr
		}
		item.Revisions = append(item.Revisions, revision)
		if revision.Revision == current {
			item.Revision = revision
		}
	}
	if err := rows.Err(); err != nil {
		return KnowledgeItem{}, err
	}
	if item.Revision.ID == "" {
		return KnowledgeItem{}, ErrUnavailable
	}
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	return item, nil
}

func (r *WorkspaceRepository) CreateKnowledge(ctx context.Context, request SaveKnowledgeRequest) (KnowledgeItem, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return KnowledgeItem{}, err
	}
	defer workspaceRollback(tx)
	consultationID, messageID, sourceType, err := validateKnowledgeSource(ctx, tx, request.SourceConsultationID, request.SourceMessageID)
	if err != nil {
		return KnowledgeItem{}, err
	}
	now := r.now().UTC()
	itemPublicID, revisionPublicID := uuid.NewString(), uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO knowledge_items
(public_id,title,status,current_revision_no,created_by,created_at,updated_at)
VALUES (?,?,'active',1,'local-owner',?,?)`, itemPublicID, request.Title, now, now)
	if err != nil {
		return KnowledgeItem{}, fmt.Errorf("persist Owner-confirmed Knowledge Item: %w", err)
	}
	itemID, err := result.LastInsertId()
	if err != nil {
		return KnowledgeItem{}, err
	}
	namespaces, _ := json.Marshal(request.Namespaces)
	resources, _ := json.Marshal(request.Resources)
	hash := knowledgeRevisionHash(request.Content, request.ClusterID, request.Environment, request.Namespaces, request.Resources, request.ReviewAt, request.ExpiresAt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO knowledge_item_revisions
(public_id,knowledge_item_id,revision_no,content,content_hash,source_type,source_consultation_id,source_message_id,
 cluster_id,environment,namespaces_json,resource_refs_json,review_at,expires_at,confirmed_by,created_at)
VALUES (?,?,1,?,?,?,?,?,?,?,?,?,?,?,'local-owner',?)`, revisionPublicID, itemID, request.Content, hash,
		sourceType, consultationID, messageID, request.ClusterID, request.Environment, namespaces, resources,
		request.ReviewAt, request.ExpiresAt, now); err != nil {
		return KnowledgeItem{}, fmt.Errorf("persist Owner-confirmed Knowledge revision: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return KnowledgeItem{}, err
	}
	return r.KnowledgeItem(ctx, itemPublicID)
}

func (r *WorkspaceRepository) UpdateKnowledge(ctx context.Context, publicID string, request UpdateKnowledgeRequest) (KnowledgeItem, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return KnowledgeItem{}, err
	}
	defer workspaceRollback(tx)
	var itemID, currentRevision uint64
	var title, status string
	err = tx.QueryRowContext(ctx, `SELECT id,title,status,current_revision_no FROM knowledge_items WHERE public_id=? FOR UPDATE`,
		strings.TrimSpace(publicID)).Scan(&itemID, &title, &status, &currentRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return KnowledgeItem{}, ErrNotFound
	}
	if err != nil {
		return KnowledgeItem{}, err
	}
	if status == "deleted" {
		return KnowledgeItem{}, ErrConflict
	}
	current, err := scanKnowledgeRevision(tx.QueryRowContext(ctx, knowledgeRevisionSelect+`
WHERE revision.knowledge_item_id=? AND revision.revision_no=?`, itemID, currentRevision))
	if err != nil {
		return KnowledgeItem{}, err
	}
	if request.Title != "" {
		title = request.Title
	}
	if request.Status != "" {
		status = request.Status
	}
	content := current.Content
	if request.Content != "" {
		content = request.Content
	}
	clusterID, environment := current.Scope.ClusterID, current.Scope.Environment
	if request.ClusterID != "" {
		clusterID = request.ClusterID
	}
	if request.Environment != "" {
		environment = request.Environment
	}
	namespaces := append([]string(nil), current.Scope.Namespaces...)
	if len(request.Namespaces) > 0 {
		namespaces = append([]string(nil), request.Namespaces...)
	}
	resources := append([]telemetry.ResourceReference(nil), current.Resources...)
	if request.Resources != nil {
		resources = append([]telemetry.ResourceReference(nil), request.Resources...)
	}
	reviewAt, expiresAt := current.ReviewAt, current.ExpiresAt
	if request.ReviewAt != nil {
		reviewAt = request.ReviewAt
	}
	if request.ExpiresAt != nil {
		expiresAt = request.ExpiresAt
	}
	hash := knowledgeRevisionHash(content, clusterID, environment, namespaces, resources, reviewAt, expiresAt)
	changed := hash != current.ContentHash
	now := r.now().UTC()
	if changed {
		namespacesJSON, _ := json.Marshal(namespaces)
		resourcesJSON, _ := json.Marshal(resources)
		if _, err = tx.ExecContext(ctx, `INSERT INTO knowledge_item_revisions
(public_id,knowledge_item_id,revision_no,content,content_hash,source_type,source_consultation_id,source_message_id,
 cluster_id,environment,namespaces_json,resource_refs_json,review_at,expires_at,confirmed_by,created_at)
VALUES (?,?,?, ?,?,'manual',NULL,NULL, ?,?,?,?,?,?,'local-owner',?)`, uuid.NewString(), itemID, currentRevision+1,
			content, hash, clusterID, environment, namespacesJSON, resourcesJSON, reviewAt, expiresAt, now); err != nil {
			return KnowledgeItem{}, fmt.Errorf("persist Knowledge Item revision: %w", err)
		}
		currentRevision++
	}
	if _, err = tx.ExecContext(ctx, `UPDATE knowledge_items SET title=?,status=?,current_revision_no=?,updated_at=? WHERE id=?`,
		title, status, currentRevision, now, itemID); err != nil {
		return KnowledgeItem{}, err
	}
	if err = tx.Commit(); err != nil {
		return KnowledgeItem{}, err
	}
	return r.KnowledgeItem(ctx, publicID)
}

func (r *WorkspaceRepository) DeleteKnowledge(ctx context.Context, publicID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE knowledge_items SET status='deleted',updated_at=? WHERE public_id=? AND status<>'deleted'`,
		r.now().UTC(), strings.TrimSpace(publicID))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		var count int
		if scanErr := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_items WHERE public_id=?`, publicID).Scan(&count); scanErr != nil {
			return scanErr
		}
		if count == 0 {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}

func (r *WorkspaceRepository) ApplicableKnowledge(ctx context.Context, snapshotID string, limit int) ([]KnowledgeRevision, error) {
	if limit < 1 || limit > 20 {
		limit = 5
	}
	var clusterID, environment string
	var namespacesJSON, resourcesJSON []byte
	if err := r.db.QueryRowContext(ctx, `SELECT cluster_id,environment,namespaces_json,resource_refs_json
FROM context_snapshots WHERE public_id=?`, snapshotID).Scan(&clusterID, &environment, &namespacesJSON, &resourcesJSON); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var namespaces []string
	var resources []telemetry.ResourceReference
	if json.Unmarshal(namespacesJSON, &namespaces) != nil || json.Unmarshal(resourcesJSON, &resources) != nil {
		return nil, ErrUnavailable
	}
	rows, err := r.db.QueryContext(ctx, knowledgeRevisionSelect+`
JOIN knowledge_items current_item ON current_item.id=revision.knowledge_item_id AND current_item.current_revision_no=revision.revision_no
WHERE current_item.status='active' AND revision.cluster_id=? AND revision.environment=?
AND (revision.expires_at IS NULL OR revision.expires_at>?) AND (revision.review_at IS NULL OR revision.review_at>?)
ORDER BY revision.created_at DESC,revision.id DESC LIMIT 50`, clusterID, environment, r.now().UTC(), r.now().UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]KnowledgeRevision, 0, limit)
	for rows.Next() {
		item, scanErr := scanKnowledgeRevision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if !knowledgeScopeMatches(item, namespaces, resources) {
			continue
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result, rows.Err()
}

const knowledgeRevisionSelect = `SELECT revision.public_id,revision.revision_no,revision.content,revision.content_hash,
revision.source_type,COALESCE(consultation.public_id,''),COALESCE(message.public_id,''),revision.cluster_id,
revision.environment,revision.namespaces_json,revision.resource_refs_json,revision.review_at,revision.expires_at,
revision.confirmed_by,revision.created_at
FROM knowledge_item_revisions revision
LEFT JOIN agent_consultations consultation ON consultation.id=revision.source_consultation_id
LEFT JOIN agent_consultation_messages message ON message.id=revision.source_message_id `

func scanKnowledgeRevision(scanner interface{ Scan(...any) error }) (KnowledgeRevision, error) {
	var item KnowledgeRevision
	var namespacesJSON, resourcesJSON []byte
	var reviewAt, expiresAt sql.NullTime
	err := scanner.Scan(&item.ID, &item.Revision, &item.Content, &item.ContentHash, &item.SourceType,
		&item.SourceConsultationID, &item.SourceMessageID, &item.Scope.ClusterID, &item.Scope.Environment,
		&namespacesJSON, &resourcesJSON, &reviewAt, &expiresAt, &item.ConfirmedBy, &item.CreatedAt)
	if err != nil {
		return KnowledgeRevision{}, err
	}
	if json.Unmarshal(namespacesJSON, &item.Scope.Namespaces) != nil || json.Unmarshal(resourcesJSON, &item.Resources) != nil {
		return KnowledgeRevision{}, ErrUnavailable
	}
	item.ReviewAt, item.ExpiresAt = workspaceOptionalTime(reviewAt), workspaceOptionalTime(expiresAt)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func validateKnowledgeSource(ctx context.Context, tx *sql.Tx, consultationPublicID, messagePublicID string) (any, any, string, error) {
	consultationPublicID, messagePublicID = strings.TrimSpace(consultationPublicID), strings.TrimSpace(messagePublicID)
	if consultationPublicID == "" && messagePublicID == "" {
		return nil, nil, "manual", nil
	}
	if consultationPublicID == "" || messagePublicID == "" {
		return nil, nil, "", ErrInvalidArgument
	}
	var consultationID, messageID uint64
	var role, status string
	err := tx.QueryRowContext(ctx, `SELECT consultation.id,message.id,message.role,message.status
FROM agent_consultations consultation JOIN agent_consultation_messages message ON message.consultation_id=consultation.id
WHERE consultation.public_id=? AND message.public_id=?`, consultationPublicID, messagePublicID).Scan(
		&consultationID, &messageID, &role, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, "", ErrNotFound
	}
	if err != nil {
		return nil, nil, "", err
	}
	if role != "assistant" || status != "completed" {
		return nil, nil, "", ErrConflict
	}
	return consultationID, messageID, "consultation", nil
}

func knowledgeRevisionHash(content, clusterID, environment string, namespaces []string, resources []telemetry.ResourceReference, reviewAt, expiresAt *time.Time) string {
	canonical, _ := json.Marshal(struct {
		Content     string                        `json:"content"`
		ClusterID   string                        `json:"cluster_id"`
		Environment string                        `json:"environment"`
		Namespaces  []string                      `json:"namespaces"`
		Resources   []telemetry.ResourceReference `json:"resource_refs"`
		ReviewAt    *time.Time                    `json:"review_at,omitempty"`
		ExpiresAt   *time.Time                    `json:"expires_at,omitempty"`
	}{content, clusterID, environment, namespaces, resources, reviewAt, expiresAt})
	return workspaceSHA256(canonical)
}

func knowledgeScopeMatches(item KnowledgeRevision, namespaces []string, resources []telemetry.ResourceReference) bool {
	for _, namespace := range item.Scope.Namespaces {
		if !slices.Contains(namespaces, namespace) {
			return false
		}
	}
	if len(item.Resources) == 0 {
		return true
	}
	for _, expected := range item.Resources {
		for _, actual := range resources {
			if expected.ID == actual.ID {
				return true
			}
		}
	}
	return false
}

var _ = settings.OperationalScope{}

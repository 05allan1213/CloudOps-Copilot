package incidentmysql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

var _ change.Repository = (*ChangeRepository)(nil)

func (s *Store) createChangeIfAbsent(ctx context.Context, item *change.Change) (bool, error) {
	if item == nil {
		return false, change.ErrInvalidArgument
	}
	if err := item.Validate(); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = item.CreatedAt
	}
	row, err := changeToRow(item)
	if err != nil {
		return false, err
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "incident_id"}, {Name: "idempotency_key"}}, DoNothing: true}).Create(&row)
	if result.Error != nil {
		return false, classifyChangeError(result.Error)
	}
	if result.RowsAffected == 0 {
		var existing changeRow
		if err := s.db.WithContext(ctx).Where("incident_id = ? AND idempotency_key = ?", item.IncidentID, item.IdempotencyKey).First(&existing).Error; err != nil {
			return false, classifyChangeError(err)
		}
		*item = changeFromRow(existing)
		return false, nil
	}
	item.ID, item.CreatedAt, item.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return true, nil
}

func (s *Store) GetChangeByPublicID(ctx context.Context, publicID string) (*change.Change, error) {
	if strings.TrimSpace(publicID) == "" {
		return nil, change.ErrInvalidArgument
	}
	var row changeRow
	if err := s.db.WithContext(ctx).Where("public_id = ?", publicID).First(&row).Error; err != nil {
		return nil, classifyChangeError(err)
	}
	item := changeFromRow(row)
	return &item, nil
}

// GetByPublicID is already the Incident repository method, so ChangeRepository adapts the shared store.
type ChangeRepository struct{ store *Store }

func NewChangeRepository(db *gorm.DB) (*ChangeRepository, error) {
	store, err := NewStore(db)
	if err != nil {
		return nil, err
	}
	return &ChangeRepository{store: store}, nil
}

func (r *ChangeRepository) CreateIfAbsent(ctx context.Context, item *change.Change) (bool, error) {
	return r.store.createChangeIfAbsent(ctx, item)
}

// PersistWithEvidence atomically persists one immutable Change and its bounded audit Evidence.
// Idempotent replays return the existing Change without duplicating Evidence.
func (r *ChangeRepository) PersistWithEvidence(ctx context.Context, item *change.Change, evidence *domain.EvidenceItem) (bool, error) {
	if item == nil || evidence == nil || evidence.IncidentID != item.IncidentID || evidence.PublicID == "" || !json.Valid(evidence.Facts) || len(evidence.Facts) > change.MaxMetadataBytes {
		return false, change.ErrInvalidArgument
	}
	created := false
	err := r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := &Store{db: tx}
		var err error
		created, err = txStore.createChangeIfAbsent(ctx, item)
		if err != nil || !created {
			return err
		}
		row := evidenceToRow(evidence)
		row.ChangeID = &item.ID
		hash := sha256.Sum256(evidence.Facts)
		row.ResultHash = hex.EncodeToString(hash[:])
		row.RedactionJSON = json.RawMessage(`{"policy":"change_evidence_bounded"}`)
		row.Valid = true
		if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
			return classifyChangeError(err)
		}
		evidence.ID, evidence.CreatedAt = row.ID, row.CreatedAt
		return nil
	})
	return created, err
}

func (r *ChangeRepository) GetByPublicID(ctx context.Context, publicID string) (*change.Change, error) {
	return r.store.GetChangeByPublicID(ctx, publicID)
}

func (r *ChangeRepository) ListByIncident(ctx context.Context, incidentPublicID string, filter change.ListFilter) (change.Page, error) {
	return r.store.ListChangesByIncident(ctx, incidentPublicID, filter)
}

func (s *Store) ListChangesByIncident(ctx context.Context, incidentPublicID string, filter change.ListFilter) (change.Page, error) {
	if strings.TrimSpace(incidentPublicID) == "" {
		return change.Page{}, change.ErrInvalidArgument
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		return change.Page{}, fmt.Errorf("%w: page size exceeds 100", change.ErrInvalidArgument)
	}
	query := s.db.WithContext(ctx).Model(&changeRow{}).
		Joins("JOIN incidents ON incidents.id = changes.incident_id").
		Where("incidents.public_id = ?", incidentPublicID)
	if filter.SourceType != "" {
		query = query.Where("changes.source_type = ?", filter.SourceType)
	}
	if filter.Status != "" {
		query = query.Where("changes.status = ?", filter.Status)
	}
	if filter.Category != "" {
		query = query.Where("changes.category = ?", filter.Category)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return change.Page{}, classifyChangeError(err)
	}
	var rows []changeRow
	if err := query.Select("changes.*").Order("COALESCE(changes.deployed_at, changes.completed_at, changes.started_at, changes.created_at) DESC, changes.id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&rows).Error; err != nil {
		return change.Page{}, classifyChangeError(err)
	}
	items := make([]change.Change, 0, len(rows))
	for _, row := range rows {
		items = append(items, changeFromRow(row))
	}
	return change.Page{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func changeToRow(item *change.Change) (changeRow, error) {
	reasons, err := json.Marshal(item.CorrelationReasons)
	if err != nil {
		return changeRow{}, fmt.Errorf("%w: correlation reasons", change.ErrInvalidArgument)
	}
	return changeRow{ID: item.ID, PublicID: item.PublicID, IncidentID: item.IncidentID, SourceType: string(item.SourceType), Repository: item.Repository, RepositoryOwner: item.RepositoryOwner, CommitSHA: item.CommitSHA, BaseCommitSHA: item.BaseCommitSHA, PullRequestNumber: item.PullRequestNumber, WorkflowRunID: item.WorkflowRunID, WorkflowName: item.WorkflowName, WorkflowConclusion: item.WorkflowConclusion, ImageRepository: item.ImageRepository, ImageTag: item.ImageTag, ImageDigest: item.ImageDigest, ImageRevision: item.ImageRevision, ArgoCDApplication: item.ArgoCDApplication, ArgoCDProject: item.ArgoCDProject, ArgoCDTargetRevision: item.ArgoCDTargetRevision, ArgoCDDeployedRevision: item.ArgoCDDeployedRevision, Environment: item.Environment, Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, WorkloadKind: item.WorkloadKind, WorkloadName: item.WorkloadName, GitOpsPath: item.GitOpsPath, StartedAt: utcPtr(item.StartedAt), CompletedAt: utcPtr(item.CompletedAt), DeployedAt: utcPtr(item.DeployedAt), Status: string(item.Status), Category: string(item.Category), ChangeSummary: item.ChangeSummary, RiskSummary: item.RiskSummary, CorrelationScore: item.CorrelationScore, CorrelationReasonsJSON: reasons, MetadataJSON: item.Metadata, Truncated: item.Truncated, Degraded: item.Degraded, IdempotencyKey: item.IdempotencyKey, CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC()}, nil
}

func changeFromRow(row changeRow) change.Change {
	var reasons []string
	_ = json.Unmarshal(row.CorrelationReasonsJSON, &reasons)
	return change.Change{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, SourceType: change.SourceType(row.SourceType), Repository: row.Repository, RepositoryOwner: row.RepositoryOwner, CommitSHA: row.CommitSHA, BaseCommitSHA: row.BaseCommitSHA, PullRequestNumber: row.PullRequestNumber, WorkflowRunID: row.WorkflowRunID, WorkflowName: row.WorkflowName, WorkflowConclusion: row.WorkflowConclusion, ImageRepository: row.ImageRepository, ImageTag: row.ImageTag, ImageDigest: row.ImageDigest, ImageRevision: row.ImageRevision, ArgoCDApplication: row.ArgoCDApplication, ArgoCDProject: row.ArgoCDProject, ArgoCDTargetRevision: row.ArgoCDTargetRevision, ArgoCDDeployedRevision: row.ArgoCDDeployedRevision, Environment: row.Environment, Cluster: row.Cluster, Namespace: row.Namespace, ServiceName: row.ServiceName, WorkloadKind: row.WorkloadKind, WorkloadName: row.WorkloadName, GitOpsPath: row.GitOpsPath, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, DeployedAt: row.DeployedAt, Status: change.Status(row.Status), Category: change.Category(row.Category), ChangeSummary: row.ChangeSummary, RiskSummary: row.RiskSummary, CorrelationScore: row.CorrelationScore, CorrelationReasons: reasons, Metadata: row.MetadataJSON, Truncated: row.Truncated, Degraded: row.Degraded, IdempotencyKey: row.IdempotencyKey, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func classifyChangeError(err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *drivermysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%w: duplicate key", change.ErrConflict)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return change.ErrNotFound
	}
	return err
}

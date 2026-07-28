package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	maxWorkspaceRunbooks    = 100
	maxWorkspaceRunbookSize = 64 * 1024
)

func (r *WorkspaceRepository) RunbookGuidance(ctx context.Context) ([]RunbookGuidance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := strings.TrimSpace(r.runbookDir)
	if dir == "" {
		return []RunbookGuidance{}, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []RunbookGuidance{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			files = append(files, entry)
		}
	}
	if len(files) > maxWorkspaceRunbooks {
		return nil, ErrUnavailable
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	result := make([]RunbookGuidance, 0, len(files))
	for _, entry := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxWorkspaceRunbookSize {
			return nil, ErrUnavailable
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		result = append(result, RunbookGuidance{
			ID: entry.Name(), Title: workspaceRunbookTitle(content, entry.Name()), Path: entry.Name(),
			Revision: hex.EncodeToString(digest[:]), Content: string(content), ModifiedAt: info.ModTime().UTC(),
		})
	}
	return result, nil
}

func (r *WorkspaceRepository) CiteWorkspaceKnowledge(ctx context.Context, lease WorkspaceLease, revisionPublicID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO agent_guidance_citations
(public_id,agent_run_id,message_id,guidance_type,knowledge_revision_id,runbook_path,runbook_revision,created_at)
SELECT ?,?,NULL,'knowledge',revision.id,NULL,NULL,? FROM knowledge_item_revisions revision
JOIN knowledge_items item ON item.id=revision.knowledge_item_id
WHERE revision.public_id=? AND item.status='active'`, uuid.NewString(), lease.RunID, r.now().UTC(), strings.TrimSpace(revisionPublicID))
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_guidance_citations citation
JOIN knowledge_item_revisions revision ON revision.id=citation.knowledge_revision_id
JOIN knowledge_items item ON item.id=revision.knowledge_item_id
WHERE citation.agent_run_id=? AND revision.public_id=? AND item.status='active'`, lease.RunID,
			strings.TrimSpace(revisionPublicID)).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrNotFound
		}
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) CiteWorkspaceRunbook(ctx context.Context, lease WorkspaceLease, path, revision string) error {
	path, revision = strings.TrimSpace(path), strings.TrimSpace(revision)
	if path == "" || filepath.Base(path) != path || len(revision) != 64 {
		return ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer workspaceRollback(tx)
	if err = r.guardWorkspaceLease(ctx, tx, lease); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO agent_guidance_citations
(public_id,agent_run_id,message_id,guidance_type,knowledge_revision_id,runbook_path,runbook_revision,created_at)
VALUES (?,?,NULL,'runbook',NULL,?,?,?)`, uuid.NewString(), lease.RunID, path, revision, r.now().UTC())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func workspaceRunbookTitle(content []byte, fallback string) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			if title := strings.TrimSpace(strings.TrimPrefix(line, "# ")); title != "" {
				return workspaceBound(title, 128)
			}
		}
	}
	return strings.TrimSuffix(fallback, filepath.Ext(fallback))
}

package settings

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

func ensurePrivateDataDirectory(dataDir string) error {
	if dataDir == "" || !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) == string(filepath.Separator) {
		return errors.New("CLOUDOPS_DATA_DIR must be a non-root absolute path")
	}
	if info, err := os.Lstat(dataDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("CLOUDOPS_DATA_DIR must not be a symbolic link")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect CloudOps data directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create CloudOps data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return fmt.Errorf("protect CloudOps data directory: %w", err)
	}
	return nil
}

func (s *Service) WriteSecret(ctx context.Context, input SecretInput) (SecretVersion, error) {
	input.Purpose = strings.TrimSpace(input.Purpose)
	if !input.Provider.Operational() || !purposePattern.MatchString(input.Purpose) {
		return SecretVersion{}, ErrInvalidDraft
	}
	if len(input.Value) < 1 || len(input.Value) > 64*1024 {
		return SecretVersion{}, errors.Join(ErrInvalidDraft, errors.New("secret value must contain 1 to 65536 bytes"))
	}
	publicID := uuid.NewString()
	relative := filepath.Join("secrets", string(input.Provider), input.Purpose, publicID)
	directory := filepath.Join(s.dataDir, filepath.Dir(relative))
	if err := ensurePrivateDirectoryTree(s.dataDir, directory); err != nil {
		return SecretVersion{}, err
	}
	path := filepath.Join(s.dataDir, relative)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return SecretVersion{}, fmt.Errorf("create secret version file: %w", err)
	}
	writeErr := func() error {
		if _, err := file.WriteString(input.Value); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		return file.Close()
	}()
	input.Value = ""
	if writeErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return SecretVersion{}, fmt.Errorf("persist secret version file: %w", writeErr)
	}
	digest, err := hashSecretFile(path)
	if err != nil {
		_ = os.Remove(path)
		return SecretVersion{}, err
	}
	fingerprint := digest[:20]
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO secret_versions (
public_id, provider, purpose, fingerprint, relative_path, state, created_by, created_at
) VALUES (?, ?, ?, ?, ?, 'configured', 'local-owner', ?)`,
		publicID, input.Provider, input.Purpose, fingerprint, filepath.ToSlash(relative), now,
	); err != nil {
		_ = os.Remove(path)
		return SecretVersion{}, fmt.Errorf("persist secret version metadata: %w", err)
	}
	return SecretVersion{
		ID: publicID, Provider: input.Provider, Purpose: input.Purpose,
		State: "configured", Fingerprint: fingerprint, CreatedAt: now,
		ReferencedBy: []string{},
	}, nil
}

func ensurePrivateDirectoryTree(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return errors.New("secret directory escapes CLOUDOPS_DATA_DIR")
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return fmt.Errorf("create private secret directory: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect private secret directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("secret directory contains a non-directory or symbolic link")
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return fmt.Errorf("protect private secret directory: %w", err)
		}
	}
	return nil
}

func hashSecretFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret version file for fingerprint: %w", err)
	}
	digest := sha256.Sum256(contents)
	for index := range contents {
		contents[index] = 0
	}
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) validateSecretReferences(ctx context.Context, draft Draft) []FieldError {
	return s.validateSecretReferencesWith(ctx, s.db, draft)
}

func (s *Service) validateSecretReferencesWith(ctx context.Context, db queryer, draft Draft) []FieldError {
	result := make([]FieldError, 0)
	byProvider := make(map[Provider]map[string]struct{}, len(draft.SecretRefs))
	for _, ref := range draft.SecretRefs {
		var provider Provider
		var purpose, state, relativePath, fingerprint string
		err := db.QueryRowContext(ctx, `SELECT provider, purpose, state, relative_path, fingerprint
FROM secret_versions WHERE public_id = ?`, ref.SecretVersionID).Scan(&provider, &purpose, &state, &relativePath, &fingerprint)
		if errors.Is(err, sql.ErrNoRows) {
			result = append(result, FieldError{Field: "secret_references", Code: "SECRET_NOT_FOUND", Message: "引用的 secret version 不存在"})
			continue
		}
		if err != nil {
			result = append(result, FieldError{Field: "secret_references", Code: "SECRET_UNAVAILABLE", Message: "无法读取 secret version metadata"})
			continue
		}
		if provider != ref.Provider || purpose != ref.Purpose || state != "configured" {
			result = append(result, FieldError{Field: "secret_references", Code: "SECRET_MISMATCH", Message: "Secret version 与 Provider purpose 不匹配"})
			continue
		}
		if err := s.verifySecretFile(relativePath, fingerprint); err != nil {
			result = append(result, FieldError{Field: "secret_references", Code: "SECRET_FILE_INVALID", Message: "Secret version 文件缺失、权限错误或 fingerprint 不匹配"})
			continue
		}
		if byProvider[provider] == nil {
			byProvider[provider] = map[string]struct{}{}
		}
		byProvider[provider][purpose] = struct{}{}
	}
	for _, provider := range draft.Providers {
		if !provider.Enabled || !providerNeedsSecret(provider.Provider) {
			continue
		}
		purpose := secretPurposeFor(provider.Provider)
		if _, ok := byProvider[provider.Provider][purpose]; !ok {
			result = append(result, FieldError{
				Field: "providers." + string(provider.Provider), Code: "SECRET_REQUIRED",
				Message: "启用该 Provider 前必须引用已配置的 write-only secret version",
			})
		}
	}
	return result
}

func (s *Service) verifySecretFile(relativePath, fingerprint string) error {
	path, err := s.resolveSecretPath(relativePath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("secret version file is not a private regular file")
	}
	digest, err := hashSecretFile(path)
	if err != nil {
		return err
	}
	if len(digest) < 20 || digest[:20] != fingerprint {
		return errors.New("secret version fingerprint mismatch")
	}
	return nil
}

func (s *Service) resolveSecretPath(relativePath string) (string, error) {
	relativePath = filepath.FromSlash(strings.TrimSpace(relativePath))
	if filepath.IsAbs(relativePath) || relativePath == "" {
		return "", errors.New("invalid secret relative path")
	}
	path := filepath.Join(s.dataDir, relativePath)
	relative, err := filepath.Rel(s.dataDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("secret path escapes CLOUDOPS_DATA_DIR")
	}
	return path, nil
}

func (s *Service) secretValue(ctx context.Context, provider Provider, refs []SecretReference) ([]byte, error) {
	purpose := secretPurposeFor(provider)
	if purpose == "" {
		return nil, nil
	}
	var publicID string
	for _, ref := range refs {
		if ref.Provider == provider && ref.Purpose == purpose {
			publicID = ref.SecretVersionID
			break
		}
	}
	if publicID == "" {
		return nil, ErrNotFound
	}
	var relativePath, fingerprint string
	if err := s.db.QueryRowContext(ctx, `SELECT relative_path, fingerprint
FROM secret_versions WHERE public_id = ? AND provider = ? AND purpose = ? AND state = 'configured'`,
		publicID, provider, purpose).Scan(&relativePath, &fingerprint); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.verifySecretFile(relativePath, fingerprint); err != nil {
		return nil, err
	}
	path, _ := s.resolveSecretPath(relativePath)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return contents, nil
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

package runbook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultMaxFiles     = 100
	defaultMaxFileBytes = 64 * 1024
)

func LoadDir(ctx context.Context, dir string, options LoadOptions) ([]Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return []Document{}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Document{}, nil
		}
		return nil, fmt.Errorf("read runbook dir: %w", err)
	}

	maxFiles := options.MaxFiles
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}
	maxBytes := options.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxFileBytes
	}

	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		files = append(files, entry)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	if len(files) > maxFiles {
		return nil, fmt.Errorf("runbook files exceed limit: got %d, max %d", len(files), maxFiles)
	}

	docs := make([]Document, 0, len(files))
	for _, entry := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat runbook %s: %w", entry.Name(), err)
		}
		if info.Size() > maxBytes {
			return nil, fmt.Errorf("runbook %s exceeds max size: got %d, max %d", entry.Name(), info.Size(), maxBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read runbook %s: %w", entry.Name(), err)
		}
		doc, err := ParseMarkdown(entry.Name(), data)
		if err != nil {
			return nil, err
		}
		doc.UpdatedAt = info.ModTime().UTC()
		docs = append(docs, doc)
	}
	return docs, nil
}

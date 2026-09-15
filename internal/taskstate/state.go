// Package taskstate opens the canonical task index read-only.
package taskstate

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var busyTimeoutMilliseconds = 30000

// CanonicalStateDatabase resolves the one supported index location. Missing
// indexes remain missing; legacy directories are never scanned or created.
func CanonicalStateDatabase(codexHome string) (string, error) {
	if !filepath.IsAbs(codexHome) {
		return "", fmt.Errorf("Codex home must be absolute")
	}
	root, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		return "", err
	}
	canonical := filepath.Join(root, "state_5.sqlite")
	if _, err := resolveContainedFile(root, canonical); err != nil {
		return "", err
	}
	// Preserve the canonical home-relative identity even when the file itself
	// is an in-home symlink, so callers can safely pass this result to Audit.
	return canonical, nil
}

// ResolveRolloutPath resolves a regular rollout file inside the real Codex
// home, rejecting escapes through either a path component or a symlink.
func ResolveRolloutPath(codexHome, path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return "", fmt.Errorf("rollout path must be an absolute JSONL path")
	}
	return resolveContainedFile(codexHome, path)
}

func resolveContainedFile(root, path string) (string, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return "", fmt.Errorf("Codex state paths must be absolute")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(realRoot, realPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside Codex state: %s", path)
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Codex state path is not a regular file: %s", path)
	}
	return realPath, nil
}

func openDatabase(ctx context.Context, database string) (*sql.DB, error) {
	if !filepath.IsAbs(database) || filepath.Base(database) != "state_5.sqlite" {
		return nil, fmt.Errorf("expected the canonical CODEX_HOME/state_5.sqlite index")
	}
	resolved, err := CanonicalStateDatabase(filepath.Dir(database))
	if err != nil {
		return nil, err
	}
	// mode=ro includes committed WAL data. immutable=1 would miss that data.
	uri := url.URL{Scheme: "file", Path: resolved}
	query := url.Values{"mode": {"ro"}}
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMilliseconds))
	query.Add("_pragma", "query_only(1)")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenReadOnly opens the canonical index with committed WAL data visible.
// The caller must close the returned database; no state is created or migrated.
func OpenReadOnly(ctx context.Context, codexHome string) (*sql.DB, error) {
	path, err := CanonicalStateDatabase(codexHome)
	if err != nil {
		return nil, err
	}
	return openDatabase(ctx, path)
}

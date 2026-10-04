package usecase

import (
	"fmt"
	"path/filepath"
	"strings"
)

// safeJoin joins a slash-separated relative path onto root and rejects results
// that would escape root (absolute paths, "../" segments). It protects against
// zip-slip and malicious paths coming from remote diffs.
func safeJoin(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	cleanRoot := filepath.Clean(root)
	joined := filepath.Join(cleanRoot, filepath.FromSlash(rel))
	if joined != cleanRoot && !strings.HasPrefix(joined, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes repository root", rel)
	}
	if joined == cleanRoot {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	return joined, nil
}

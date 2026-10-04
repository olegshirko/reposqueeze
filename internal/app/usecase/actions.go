package usecase

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// buildCommitActions converts local file changes into GitLab Commits API actions.
// content returns the new content of a path; exists reports whether a path is
// already present on the target GitLab branch (it decides create vs update and
// skips deletes of files GitLab does not have).
func buildCommitActions(
	files []gateway.CommitFileInfo,
	content func(path string) ([]byte, error),
	exists func(path string) bool,
) ([]gateway.CommitAction, error) {
	var actions []gateway.CommitAction

	upsert := func(path string) error {
		data, err := content(path)
		if err != nil {
			return fmt.Errorf("failed to get file content for %q: %w", path, err)
		}
		gitPath := filepath.ToSlash(path)
		action := "create"
		if exists(gitPath) {
			action = "update"
		}
		actions = append(actions, gateway.CommitAction{
			Action:   action,
			FilePath: gitPath,
			Content:  string(data),
			Encoding: "text",
		})
		return nil
	}

	remove := func(path string) {
		gitPath := filepath.ToSlash(path)
		if exists(gitPath) {
			actions = append(actions, gateway.CommitAction{Action: "delete", FilePath: gitPath})
		}
	}

	for _, f := range files {
		switch {
		case f.Status == "A" || f.Status == "M" || strings.HasPrefix(f.Status, "T"):
			if err := upsert(f.Path); err != nil {
				return nil, err
			}
		case f.Status == "D":
			remove(f.Path)
		case strings.HasPrefix(f.Status, "R"):
			remove(f.OldPath)
			if err := upsert(f.Path); err != nil {
				return nil, err
			}
		case strings.HasPrefix(f.Status, "C"):
			if err := upsert(f.Path); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported file change status %q for file %q", f.Status, f.Path)
		}
	}

	return actions, nil
}

package usecase

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// projectNameFromPath derives the GitLab project name from a local repository path.
func projectNameFromPath(repoPath string) string {
	return filepath.Base(strings.TrimSuffix(filepath.Clean(repoPath), ".git"))
}

// resolveProject finds the GitLab project that matches the local repository folder name.
// A missing project is reported as an error.
func resolveProject(gw gateway.GitLabGateway, repoPath string) (*entity.Project, error) {
	name := projectNameFromPath(repoPath)
	project, err := gw.FindProjectByName(name)
	if err != nil {
		return nil, fmt.Errorf("failed to find project: %w", err)
	}
	if project == nil {
		return nil, fmt.Errorf("project %q not found on GitLab", name)
	}
	return project, nil
}

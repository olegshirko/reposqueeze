// Package state stores sync mirrors inside the repository's .git directory,
// so the data never ends up in commits.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

const (
	dirName       = "reposqueeze"
	fileName      = "mirrors.json"
	formatVersion = 1
)

// FileStore keeps mirrors in <git-dir>/reposqueeze/mirrors.json.
type FileStore struct {
	git gateway.SyncGit
}

var _ gateway.MirrorStore = (*FileStore)(nil)

// NewFileStore creates a store that locates .git through git itself
// (works for worktrees and submodules too).
func NewFileStore(git gateway.SyncGit) *FileStore {
	return &FileStore{git: git}
}

func (s *FileStore) path(repoPath string) (string, error) {
	gitDir, err := s.git.GitDir(repoPath)
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return filepath.Join(gitDir, dirName, fileName), nil
}

// Load reads the mirror set; a missing file yields an empty set.
func (s *FileStore) Load(repoPath string) (*entity.MirrorSet, error) {
	p, err := s.path(repoPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &entity.MirrorSet{Version: formatVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var set entity.MirrorSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("corrupted %s: %w", p, err)
	}
	return &set, nil
}

// Save writes the mirror set atomically.
func (s *FileStore) Save(repoPath string, set *entity.MirrorSet) error {
	p, err := s.path(repoPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	set.Version = formatVersion
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), fileName+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

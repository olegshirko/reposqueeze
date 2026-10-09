package usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

const (
	clipPackage  = "clipboard"
	clipVersion  = "latest"
	clipFile     = "clip.bin"
	clipMetaFile = "meta.json"
)

// ClipGitLab is what the API-based clip store needs from GitLab: a private
// project and its generic package registry.
type ClipGitLab interface {
	FindProjectByName(name string) (*entity.Project, error)
	CreatePrivateProject(name string) (*entity.Project, error)
	gateway.GenericPackages
}

// PackageClipStore keeps the clipboard in the generic package registry of a
// private project (needs a GitLab API token). Only the newest copy is kept.
type PackageClipStore struct {
	gitlab  ClipGitLab
	project string
}

// NewPackageClipStore uses the given project ("" = DefaultClipProject).
func NewPackageClipStore(gitlab ClipGitLab, project string) *PackageClipStore {
	if project == "" {
		project = DefaultClipProject
	}
	return &PackageClipStore{gitlab: gitlab, project: project}
}

func (s *PackageClipStore) projectID(create bool) (int, error) {
	p, err := s.gitlab.FindProjectByName(s.project)
	if err != nil {
		return 0, err
	}
	if p == nil && create {
		p, err = s.gitlab.CreatePrivateProject(s.project)
		if err != nil {
			return 0, err
		}
	}
	if p == nil {
		return 0, ErrNoClip
	}
	return p.ID, nil
}

// Put uploads the clipboard and removes older copies.
func (s *PackageClipStore) Put(ctx context.Context, blob, meta []byte) error {
	id, err := s.projectID(true)
	if err != nil {
		return err
	}
	if err := s.gitlab.UploadPackageFile(id, clipPackage, clipVersion, clipFile, blob); err != nil {
		return err
	}
	if err := s.gitlab.UploadPackageFile(id, clipPackage, clipVersion, clipMetaFile, meta); err != nil {
		return err
	}
	return s.prune(id)
}

// prune keeps only the newest copy of each file.
func (s *PackageClipStore) prune(projectID int) error {
	files, err := s.gitlab.ListPackageFiles(projectID, clipPackage, clipVersion)
	if err != nil {
		return err
	}
	newest := map[string]int{}
	for _, f := range files {
		if f.ID > newest[f.FileName] {
			newest[f.FileName] = f.ID
		}
	}
	var errs []error
	for _, f := range files {
		if f.ID != newest[f.FileName] {
			if err := s.gitlab.DeletePackageFile(projectID, f.PackageID, f.ID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("old copies were not removed: %w", err)
	}
	return nil
}

// Get downloads the clipboard.
func (s *PackageClipStore) Get(ctx context.Context) ([]byte, []byte, error) {
	id, err := s.projectID(false)
	if err != nil {
		return nil, nil, err
	}
	blob, err := s.gitlab.DownloadPackageFile(id, clipPackage, clipVersion, clipFile)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNoClip, err)
	}
	meta, _ := s.gitlab.DownloadPackageFile(id, clipPackage, clipVersion, clipMetaFile)
	return blob, meta, nil
}

// Version is the id of the newest clipboard file.
func (s *PackageClipStore) Version(ctx context.Context) (string, error) {
	id, err := s.projectID(false)
	if errors.Is(err, ErrNoClip) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	files, err := s.gitlab.ListPackageFiles(id, clipPackage, clipVersion)
	if err != nil {
		return "", err
	}
	v := ""
	for _, f := range files {
		if f.FileName == clipFile {
			v = strconv.Itoa(f.ID)
		}
	}
	return v, nil
}

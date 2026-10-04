package usecase

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

// applyRemoteDiff mirrors GitLab diff entries into the local working tree:
// deleted files are removed, renamed files lose their old path, everything
// else is downloaded at ref. It returns the number of downloaded files.
func applyRemoteDiff(gw gateway.GitLabGateway, log logger.Logger, projectID int, ref, root string, diffs []gateway.DiffEntry) (int, error) {
	downloaded := 0
	for _, d := range diffs {
		if d.DeletedFile {
			if err := removeLocal(root, d.NewPath); err != nil {
				return downloaded, err
			}
			log.Infof("Deleted: %s", d.NewPath)
			continue
		}

		if d.RenamedFile && d.OldPath != d.NewPath {
			if err := removeLocal(root, d.OldPath); err != nil {
				return downloaded, err
			}
			log.Infof("Deleted (rename): %s", d.OldPath)
		}

		if err := downloadRemoteFile(gw, log, projectID, d.NewPath, ref, root); err != nil {
			return downloaded, err
		}
		downloaded++
	}
	return downloaded, nil
}

func removeLocal(root, rel string) error {
	localPath, err := safeJoin(root, rel)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(localPath); err != nil {
		return fmt.Errorf("failed to delete file %q: %w", rel, err)
	}
	return nil
}

// downloadRemoteFile fetches filePath at ref from GitLab and writes it under root.
func downloadRemoteFile(gw gateway.GitLabGateway, log logger.Logger, projectID int, filePath, ref, root string) error {
	content, err := gw.GetRawFile(projectID, filePath, ref)
	if err != nil {
		return fmt.Errorf("failed to download file %q: %w", filePath, err)
	}
	if err := writeLocalFile(root, filePath, content); err != nil {
		return err
	}
	log.Infof("Pulled: %s", filePath)
	return nil
}

// writeLocalFile writes content to root/rel, creating parent directories.
func writeLocalFile(root, rel string, content []byte) error {
	localPath, err := safeJoin(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %q: %w", rel, err)
	}
	if err := os.WriteFile(localPath, content, 0o644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", rel, err)
	}
	return nil
}

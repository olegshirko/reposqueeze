package usecase

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
)

type CreateOrphanBranchFromGitlabUseCase struct {
	GitGateway    gateway.GitGateway
	GitLabGateway gateway.GitLabGateway
	logger        logger.Logger
}

type CreateOrphanBranchFromGitlabInput struct {
	RepoPath   string
	BranchName string
}

func NewCreateOrphanBranchFromGitlabUseCase(
	gitGateway gateway.GitGateway,
	gitLabGateway gateway.GitLabGateway,
	log logger.Logger,
) *CreateOrphanBranchFromGitlabUseCase {
	return &CreateOrphanBranchFromGitlabUseCase{
		GitGateway:    gitGateway,
		GitLabGateway: gitLabGateway,
		logger:        log,
	}
}

func (uc *CreateOrphanBranchFromGitlabUseCase) Execute(ctx context.Context, input CreateOrphanBranchFromGitlabInput) (time.Duration, int, error) {
	projectName := filepath.Base(strings.TrimSuffix(input.RepoPath, ".git"))
	uc.logger.Info(projectName)
	project, err := uc.GitLabGateway.FindProjectByName(projectName)
	if err != nil {
		return 0, 0, err
	}
	if project == nil {
		uc.logger.Infof("project %s not found", projectName)
		return 0, 0, nil
	}

	repo := &entity.Repository{Path: input.RepoPath}
	branch := &entity.Branch{Name: input.BranchName}
	if err := uc.GitGateway.CreateEmptyOrphanBranch(ctx, repo, branch, ""); err != nil {
		return 0, 0, err
	}

	if err = uc.GitGateway.CleanWorkdir(input.RepoPath); err != nil {
		return 0, 0, err
	}

	buffer := new(bytes.Buffer)
	if err = uc.GitLabGateway.DownloadRepoArchive(project.ID, buffer); err != nil {
		return 0, 0, err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		return 0, 0, err
	}

	var filesCount int
	for _, file := range zipReader.File {
		if !strings.HasSuffix(file.Name, "/") {
			filesCount++
		}
	}

	if len(zipReader.File) > 0 {
		firstFile := zipReader.File[0]
		rootDir := strings.Split(firstFile.Name, "/")[0] + "/"

		for _, file := range zipReader.File {
			if !strings.HasPrefix(file.Name, rootDir) {
				continue
			}
			relativePath := strings.TrimPrefix(file.Name, rootDir)
			if relativePath == "" {
				continue
			}

			extractedFilePath := filepath.Join(input.RepoPath, relativePath)

			if file.FileInfo().IsDir() {
				os.MkdirAll(extractedFilePath, file.Mode())
			} else {
				zippedFile, err := file.Open()
				if err != nil {
					return 0, 0, err
				}
				defer zippedFile.Close()

				outputFile, err := os.OpenFile(
					extractedFilePath,
					os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
					file.Mode(),
				)
				if err != nil {
					return 0, 0, err
				}
				defer outputFile.Close()

				_, err = io.Copy(outputFile, zippedFile)
				if err != nil {
					return 0, 0, err
				}
			}
		}
	}

	commitMessage := "Add project files to orphan branch " + input.BranchName
	startTime := time.Now()
	if err = uc.GitGateway.Commit(input.RepoPath, commitMessage); err != nil {
		return 0, 0, err
	}
	duration := time.Since(startTime)

	return duration, filesCount, nil
}

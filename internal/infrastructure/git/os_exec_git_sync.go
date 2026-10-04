package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

var _ gateway.SyncGit = (*OSExecGitGateway)(nil)

// git runs a git command in repoPath and returns stdout. Stderr is included in the error.
func (g *OSExecGitGateway) git(repoPath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (g *OSExecGitGateway) gitString(repoPath string, args ...string) (string, error) {
	out, err := g.git(repoPath, args...)
	return strings.TrimSpace(string(out)), err
}

// RevParse resolves ref to a full commit SHA.
func (g *OSExecGitGateway) RevParse(repoPath, ref string) (string, error) {
	return g.gitString(repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// CurrentBranch returns the checked-out branch name; it fails on a detached HEAD.
func (g *OSExecGitGateway) CurrentBranch(repoPath string) (string, error) {
	return g.gitString(repoPath, "symbolic-ref", "--short", "HEAD")
}

// IsClean reports whether the working tree has no staged, unstaged or untracked changes.
func (g *OSExecGitGateway) IsClean(repoPath string) (bool, error) {
	out, err := g.gitString(repoPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// GitDir returns the absolute path of the repository's .git directory.
func (g *OSExecGitGateway) GitDir(repoPath string) (string, error) {
	return g.gitString(repoPath, "rev-parse", "--absolute-git-dir")
}

// DiffFiles lists changed files between two refs, without rename detection.
func (g *OSExecGitGateway) DiffFiles(repoPath, from, to string) ([]gateway.CommitFileInfo, error) {
	out, err := g.git(repoPath, "diff", "--name-status", "--no-renames", "-z", from, to)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var result []gateway.CommitFileInfo
	for i := 0; i+1 < len(fields); i += 2 {
		result = append(result, gateway.CommitFileInfo{Status: fields[i], Path: fields[i+1]})
	}
	return result, nil
}

// FileAtRef returns the content of path at ref.
func (g *OSExecGitGateway) FileAtRef(repoPath, ref, path string) ([]byte, bool, error) {
	spec := ref + ":" + path
	if _, err := g.git(repoPath, "cat-file", "-e", spec); err != nil {
		return nil, false, nil
	}
	out, err := g.git(repoPath, "cat-file", "blob", spec)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// MergeFile performs a three-way merge with `git merge-file`. Conflicts are
// marked with diff3-style markers using labels (ours, base, theirs).
func (g *OSExecGitGateway) MergeFile(ours, base, theirs []byte, labels [3]string) (gateway.MergeResult, error) {
	dir, err := os.MkdirTemp("", "reposqueeze-merge-")
	if err != nil {
		return gateway.MergeResult{}, err
	}
	defer os.RemoveAll(dir)

	paths := [3]string{filepath.Join(dir, "ours"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs")}
	for i, content := range [][]byte{ours, base, theirs} {
		if err := os.WriteFile(paths[i], content, 0o600); err != nil {
			return gateway.MergeResult{}, err
		}
	}

	cmd := exec.Command("git", "merge-file", "-p", "--diff3",
		"-L", labels[0], "-L", labels[1], "-L", labels[2],
		paths[0], paths[1], paths[2])
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return gateway.MergeResult{Content: stdout.Bytes()}, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() > 0 && exitErr.ExitCode() < 128:
		// A positive exit code is the number of conflicts.
		return gateway.MergeResult{Content: stdout.Bytes(), Conflicts: true}, nil
	default:
		return gateway.MergeResult{}, fmt.Errorf("git merge-file: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
}

// CommitPaths stages the given paths (additions, modifications and deletions) and commits them.
func (g *OSExecGitGateway) CommitPaths(repoPath, message string, paths []string, allowEmpty bool) (string, error) {
	if len(paths) > 0 {
		args := append([]string{"add", "-A", "--"}, paths...)
		if _, err := g.git(repoPath, args...); err != nil {
			return "", err
		}
	}
	args := []string{"commit", "-q", "-m", message}
	if allowEmpty {
		args = append(args, "--allow-empty")
	}
	if _, err := g.git(repoPath, args...); err != nil {
		return "", err
	}
	return g.RevParse(repoPath, "HEAD")
}

// RestorePaths puts the given paths back to their HEAD state.
func (g *OSExecGitGateway) RestorePaths(repoPath string, paths []string) error {
	var tracked []string
	for _, p := range paths {
		if _, found, _ := g.FileAtRef(repoPath, "HEAD", p); found {
			tracked = append(tracked, p)
			continue
		}
		if err := os.RemoveAll(filepath.Join(repoPath, filepath.FromSlash(p))); err != nil {
			return err
		}
	}
	if len(tracked) == 0 {
		return nil
	}
	_, err := g.git(repoPath, append([]string{"checkout", "HEAD", "--"}, tracked...)...)
	return err
}

// StashPush stashes local changes, including untracked files.
func (g *OSExecGitGateway) StashPush(repoPath, message string) (bool, error) {
	clean, err := g.IsClean(repoPath)
	if err != nil || clean {
		return false, err
	}
	if _, err := g.git(repoPath, "stash", "push", "--include-untracked", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}

// StashPop re-applies the latest stash.
func (g *OSExecGitGateway) StashPop(repoPath string) error {
	_, err := g.git(repoPath, "stash", "pop")
	return err
}

// LogCommits returns up to limit commits reachable from ref, newest first.
func (g *OSExecGitGateway) LogCommits(repoPath, ref string, limit int) ([]gateway.CommitInfo, error) {
	out, err := g.git(repoPath, "log", "-n", strconv.Itoa(limit), "--format=%H%x00%s%x00", ref)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "\x00")
	var result []gateway.CommitInfo
	for i := 0; i+1 < len(fields); i += 2 {
		result = append(result, gateway.CommitInfo{ID: strings.TrimSpace(fields[i]), Message: fields[i+1]})
	}
	return result, nil
}

// FindLastTrailer returns the newest commit reachable from ref that carries the trailer key.
func (g *OSExecGitGateway) FindLastTrailer(repoPath, ref, key string) (string, string, error) {
	format := fmt.Sprintf("--format=%%H%%x00%%(trailers:key=%s,valueonly,separator=%%x2C)%%x00", key)
	out, err := g.git(repoPath, "log", format, ref)
	if err != nil {
		return "", "", err
	}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		value := strings.TrimSpace(fields[i+1])
		if value != "" {
			return strings.TrimSpace(fields[i]), value, nil
		}
	}
	return "", "", nil
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (g *OSExecGitGateway) IsAncestor(repoPath, ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "-C", repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return false, nil
	default:
		return false, err
	}
}

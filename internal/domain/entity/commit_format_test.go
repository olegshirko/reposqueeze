package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommitFormat_Apply(t *testing.T) {
	f := CommitFormat{Type: "fix", Task: "TASK-NUMBER01"}
	assert.Equal(t, "fix: commit message TASK-NUMBER01", f.Apply("commit message"))
	assert.Equal(t, "fix: add login TASK-NUMBER01", f.Apply("feat(auth)!: add login"))
	assert.Equal(t, "fix: already TASK-NUMBER01 here", f.Apply("already TASK-NUMBER01 here"))
	assert.Equal(t, "fix: title TASK-NUMBER01\n\nbody\n\nTrailer: x", f.Apply("title\n\nbody\n\nTrailer: x"))

	assert.Equal(t, "feat: keep TASK-1", CommitFormat{Task: "TASK-1"}.Apply("feat: keep"))
	assert.Equal(t, "test: only type", CommitFormat{Type: "test"}.Apply("only type"))
	assert.Equal(t, "untouched", CommitFormat{}.Apply("untouched"))
	assert.Equal(t, "fix: commit message TASK 01 (backend)",
		CommitFormat{Type: "fix", Task: "  TASK 01 (backend) "}.Apply("commit message"))
}

func TestCommitFormat_ValidateAndMerge(t *testing.T) {
	assert.NoError(t, CommitFormat{Type: "feat", Task: "ABC-1"}.Validate())
	assert.Error(t, CommitFormat{Type: "Fix it"}.Validate())
	assert.NoError(t, CommitFormat{Task: "PROJ 123 login form"}.Validate())
	assert.Error(t, CommitFormat{Task: "ABC\n1"}.Validate())

	got := CommitFormat{Task: "NEW-2"}.Merge(CommitFormat{Type: "fix", Task: "OLD-1"})
	assert.Equal(t, CommitFormat{Type: "fix", Task: "NEW-2"}, got)
}

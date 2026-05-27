package gitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestHTTPGitLabGateway_CommitFilesViaAPI(t *testing.T) {
	t.Run("successful commit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects/123/repository/commits", r.URL.Path)
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))

			var payload commitPayload
			err := json.NewDecoder(r.Body).Decode(&payload)
			assert.NoError(t, err)
			assert.Equal(t, "test-branch", payload.Branch)
			assert.Equal(t, "test-commit", payload.CommitMessage)
			assert.Len(t, payload.Actions, 1)
			assert.Equal(t, "create", payload.Actions[0].Action)
			assert.Equal(t, "file.txt", payload.Actions[0].FilePath)

			w.WriteHeader(http.StatusCreated)
			fmt.Fprintln(w, `{}`)
		}))
		defer server.Close()

		g := &HTTPGitLabGateway{
			Client:  server.Client(),
			Token:   "test-token",
			BaseURL: server.URL + "/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}

		actions := []gateway.CommitAction{
			{
				Action:   "create",
				FilePath: "file.txt",
				Content:  "hello world",
			},
		}

		err := g.CommitFilesViaAPI("123", "test-branch", "test-commit", actions)
		assert.NoError(t, err)
	})

	t.Run("api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		g := &HTTPGitLabGateway{
			Client:  server.Client(),
			Token:   "test-token",
			BaseURL: server.URL + "/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}

		err := g.CommitFilesViaAPI("123", "test-branch", "test-commit", []gateway.CommitAction{})
		assert.Error(t, err)
	})

	t.Run("invalid url", func(t *testing.T) {
		g := &HTTPGitLabGateway{
			Client:  http.DefaultClient,
			Token:   "test-token",
			BaseURL: "http://127.0.0.1:1/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}
		// The default client will fail on an invalid URL, but we can't easily inject a bad URL
		// into the method itself. Instead, we rely on the fact that a non-existent server will fail.
		// This test is somewhat limited but demonstrates the error path.
		// A better approach would be to make the base URL configurable in HTTPGitLabGateway.
		err := g.CommitFilesViaAPI("123", "test-branch", "test-commit", []gateway.CommitAction{})
		assert.Error(t, err)
	})
}

func TestHTTPGitLabGateway_GetCompareDiff(t *testing.T) {
	t.Run("successful compare", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects/42/repository/compare", r.URL.Path)
			assert.Equal(t, "GET", r.Method)
			assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
			assert.Equal(t, "abc123", r.URL.Query().Get("from"))
			assert.Equal(t, "main", r.URL.Query().Get("to"))

			resp := map[string]interface{}{
				"diffs": []gateway.DiffEntry{
					{NewPath: "README.md", NewFile: true},
					{NewPath: "old.go", DeletedFile: true},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		g := &HTTPGitLabGateway{
			Client:  server.Client(),
			Token:   "test-token",
			BaseURL: server.URL + "/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}

		diffs, err := g.GetCompareDiff(42, "abc123", "main")
		assert.NoError(t, err)
		assert.Len(t, diffs, 2)
		assert.Equal(t, "README.md", diffs[0].NewPath)
		assert.True(t, diffs[1].DeletedFile)
	})

	t.Run("api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		g := &HTTPGitLabGateway{
			Client:  server.Client(),
			Token:   "test-token",
			BaseURL: server.URL + "/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}

		_, err := g.GetCompareDiff(42, "abc123", "main")
		assert.Error(t, err)
	})
}
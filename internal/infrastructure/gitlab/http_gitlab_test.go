package gitlab

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			fmt.Fprintln(w, `{"id":"abc123","parent_ids":["p1"]}`)
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

		created, err := g.CommitFilesViaAPI("123", "test-branch", "test-commit", actions)
		assert.NoError(t, err)
		assert.Equal(t, "abc123", created.ID)
		assert.Equal(t, []string{"p1"}, created.ParentIDs)
		// The caller's slice must not be re-encoded in place.
		assert.Equal(t, "hello world", actions[0].Content)
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

		_, err := g.CommitFilesViaAPI("123", "test-branch", "test-commit", []gateway.CommitAction{})
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
		_, err := g.CommitFilesViaAPI("123", "test-branch", "test-commit", []gateway.CommitAction{})
		assert.Error(t, err)
	})
}

func TestHTTPGitLabGateway_GetCommitDiff(t *testing.T) {
	t.Run("successful diff", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v4/projects/42/repository/commits/sha123/diff", r.URL.Path)
			assert.Equal(t, "GET", r.Method)
			assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))
			assert.Equal(t, "1", r.URL.Query().Get("page"))

			diffs := []gateway.DiffEntry{
				{NewPath: "README.md", NewFile: true},
				{NewPath: "main.go", NewFile: true},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(diffs)
		}))
		defer server.Close()

		g := &HTTPGitLabGateway{
			Client:  server.Client(),
			Token:   "test-token",
			BaseURL: server.URL + "/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}

		diffs, err := g.GetCommitDiff(42, "sha123")
		assert.NoError(t, err)
		assert.Len(t, diffs, 2)
		assert.Equal(t, "README.md", diffs[0].NewPath)
	})

	t.Run("pagination", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			page := r.URL.Query().Get("page")
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))

			var diffs []gateway.DiffEntry
			if page == "1" {
				for i := 0; i < 100; i++ {
					diffs = append(diffs, gateway.DiffEntry{NewPath: fmt.Sprintf("file_%d.txt", i)})
				}
			} else if page == "2" {
				diffs = append(diffs, gateway.DiffEntry{NewPath: "last_file.txt"})
			} else {
				t.Fatalf("unexpected page: %s", page)
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(diffs)
		}))
		defer server.Close()

		g := &HTTPGitLabGateway{
			Client:  server.Client(),
			Token:   "test-token",
			BaseURL: server.URL + "/api/v4",
			logger:  logger.NewLoggerWithWriter(logrus.New().Out),
		}

		diffs, err := g.GetCommitDiff(42, "sha123")
		assert.NoError(t, err)
		assert.Len(t, diffs, 101)
		assert.Equal(t, "last_file.txt", diffs[100].NewPath)
		assert.Equal(t, 2, callCount)
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

		_, err := g.GetCommitDiff(42, "sha123")
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
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))
			assert.Equal(t, "1", r.URL.Query().Get("page"))

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

	t.Run("pagination", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			page := r.URL.Query().Get("page")
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))

			var diffs []gateway.DiffEntry
			if page == "1" {
				// return exactly 100 items to trigger next page
				for i := 0; i < 100; i++ {
					diffs = append(diffs, gateway.DiffEntry{NewPath: fmt.Sprintf("file_%d.txt", i)})
				}
			} else if page == "2" {
				diffs = append(diffs, gateway.DiffEntry{NewPath: "file_extra.txt"})
			} else {
				t.Fatalf("unexpected page: %s", page)
			}

			resp := map[string]interface{}{"diffs": diffs}
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
		assert.Len(t, diffs, 101)
		assert.Equal(t, "file_extra.txt", diffs[100].NewPath)
		assert.Equal(t, 2, callCount)
	})
}

func TestNormalizeBaseURL(t *testing.T) {
	assert.Equal(t, "", NormalizeBaseURL(""))
	assert.Equal(t, "https://git.example.com/api/v4", NormalizeBaseURL("https://git.example.com"))
	assert.Equal(t, "https://git.example.com/api/v4", NormalizeBaseURL("https://git.example.com/"))
	assert.Equal(t, "https://git.example.com/api/v4", NormalizeBaseURL("https://git.example.com/api/v4"))
}

func TestHTTPGitLabGateway_ListCommitsAfter(t *testing.T) {
	// 150 commits on the branch, newest first: c149 ... c0.
	all := make([]gateway.CommitInfo, 150)
	for i := range all {
		all[i] = gateway.CommitInfo{ID: fmt.Sprintf("c%03d%036d", 149-i, 0)}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "true", q.Get("first_parent"))
		assert.Equal(t, "release", q.Get("ref_name"))
		page, _ := strconv.Atoi(q.Get("page"))
		start, end := (page-1)*100, page*100
		if end > len(all) {
			end = len(all)
		}
		if start > len(all) {
			start = len(all)
		}
		json.NewEncoder(w).Encode(all[start:end])
	}))
	defer server.Close()

	g := &HTTPGitLabGateway{Client: server.Client(), Token: "t", BaseURL: server.URL + "/api/v4",
		logger: logger.NewLoggerWithWriter(logrus.New().Out)}

	// "after" on the second page: c020 -> returns c021..c149, oldest first.
	got, err := g.ListCommitsAfter(1, "release", all[129].ID, 1000)
	assert.NoError(t, err)
	assert.Len(t, got, 129)
	assert.Equal(t, all[128].ID, got[0].ID)
	assert.Equal(t, all[0].ID, got[len(got)-1].ID)

	// Short SHA works, head itself yields nothing.
	got, err = g.ListCommitsAfter(1, "release", all[0].ID[:10], 1000)
	assert.NoError(t, err)
	assert.Empty(t, got)

	_, err = g.ListCommitsAfter(1, "release", "deadbeef", 1000)
	assert.Error(t, err)

	_, err = g.ListCommitsAfter(1, "release", all[129].ID, 50)
	assert.Error(t, err)
}

func TestHTTPGitLabGateway_GenericPackages(t *testing.T) {
	stored := map[string][]byte{}
	var files []map[string]interface{}
	deleted := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "t", r.Header.Get("PRIVATE-TOKEN"))
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPut && p == "/api/v4/projects/5/packages/generic/clipboard/latest/clip.bin":
			body, _ := io.ReadAll(r.Body)
			stored["clip.bin"] = body
			files = append(files, map[string]interface{}{"id": len(files) + 10, "file_name": "clip.bin"})
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && p == "/api/v4/projects/5/packages/generic/clipboard/latest/clip.bin":
			w.Write(stored["clip.bin"])
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/v4/projects/5/packages/generic/"):
			http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
		case r.Method == http.MethodGet && p == "/api/v4/projects/5/packages":
			assert.Equal(t, "generic", r.URL.Query().Get("package_type"))
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 3, "name": "clipboard", "version": "latest"},
				{"id": 4, "name": "clipboard", "version": "other"},
			})
		case r.Method == http.MethodGet && p == "/api/v4/projects/5/packages/3/package_files":
			json.NewEncoder(w).Encode(files)
		case r.Method == http.MethodDelete && strings.HasPrefix(p, "/api/v4/projects/5/packages/3/package_files/"):
			deleted = append(deleted, strings.TrimPrefix(p, "/api/v4/projects/5/packages/3/package_files/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, p)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer server.Close()
	g := &HTTPGitLabGateway{Client: server.Client(), Token: "t", BaseURL: server.URL + "/api/v4",
		logger: logger.NewLoggerWithWriter(logrus.New().Out)}

	blob := []byte("Salted__\x00\x01binary\xff")
	require.NoError(t, g.UploadPackageFile(5, "clipboard", "latest", "clip.bin", blob))
	require.NoError(t, g.UploadPackageFile(5, "clipboard", "latest", "clip.bin", blob))
	got, err := g.DownloadPackageFile(5, "clipboard", "latest", "clip.bin")
	require.NoError(t, err)
	assert.Equal(t, blob, got, "binary data survives as is")

	list, err := g.ListPackageFiles(5, "clipboard", "latest")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, 3, list[0].PackageID)
	require.NoError(t, g.DeletePackageFile(5, 3, list[0].ID))
	assert.Equal(t, []string{"10"}, deleted)

	_, err = g.DownloadPackageFile(5, "clipboard", "latest", "missing.bin")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
}

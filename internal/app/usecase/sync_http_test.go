package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/gitlab"
	"github.com/olegshirko/reposqueeze/internal/infrastructure/state"
)

// serveFakeGitLab exposes fakeGitLab through the subset of the GitLab REST API
// used by sync, so the real HTTP gateway is exercised end to end.
func serveFakeGitLab(t *testing.T, f *fakeGitLab) *httptest.Server {
	t.Helper()
	writeJSON := func(w http.ResponseWriter, status int, v interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(v))
	}
	prefix := "/api/v4/projects/" + strconv.Itoa(f.project.ID) + "/repository/"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "secret-token-xyz", r.Header.Get("PRIVATE-TOKEN"))
		q := r.URL.Query()
		path := r.URL.EscapedPath()

		switch {
		case path == "/api/v4/projects" && r.Method == http.MethodGet:
			writeJSON(w, 200, []map[string]interface{}{{"id": f.project.ID, "name": f.project.Name}})

		case path == prefix+"commits" && r.Method == http.MethodGet:
			limit, _ := strconv.Atoi(q.Get("per_page"))
			commits, err := f.GetCommits(f.project.ID, q.Get("ref_name"), limit)
			if err != nil {
				writeJSON(w, 404, map[string]string{"message": err.Error()})
				return
			}
			writeJSON(w, 200, commits)

		case path == prefix+"commits" && r.Method == http.MethodPost:
			var payload struct {
				Branch        string                 `json:"branch"`
				CommitMessage string                 `json:"commit_message"`
				Actions       []gateway.CommitAction `json:"actions"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			for i, a := range payload.Actions {
				require.Equal(t, "base64", a.Encoding)
				raw, err := base64.StdEncoding.DecodeString(a.Content)
				require.NoError(t, err)
				payload.Actions[i].Content = string(raw)
			}
			created, err := f.CommitFilesViaAPI(strconv.Itoa(f.project.ID), payload.Branch, payload.CommitMessage, payload.Actions)
			if err != nil {
				writeJSON(w, 400, map[string]string{"message": err.Error()})
				return
			}
			writeJSON(w, 201, created)

		case path == prefix+"compare":
			var diffs []gateway.DiffEntry
			if q.Get("page") == "1" {
				var err error
				if diffs, err = f.GetCompareDiff(f.project.ID, q.Get("from"), q.Get("to")); err != nil {
					writeJSON(w, 404, map[string]string{"message": err.Error()})
					return
				}
			}
			writeJSON(w, 200, map[string]interface{}{"diffs": diffs})

		case strings.HasPrefix(path, prefix+"files/") && strings.HasSuffix(path, "/raw"):
			escaped := strings.TrimSuffix(strings.TrimPrefix(path, prefix+"files/"), "/raw")
			require.NotContains(t, escaped, "/", "file paths must be fully URL-encoded")
			file, err := url.PathUnescape(escaped)
			require.NoError(t, err)
			content, err := f.GetRawFile(f.project.ID, file, q.Get("ref"))
			if err != nil {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(200)
			if r.Method != http.MethodHead {
				w.Write(content)
			}

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
}

func TestSync_OverHTTPGateway(t *testing.T) {
	e := newSyncEnv(t, map[string]string{"a.txt": "a", "dir/sub/b.txt": "b\n", "keep.txt": "k"})
	srv := serveFakeGitLab(t, e.gl)
	defer srv.Close()

	gl := gitlab.NewHTTPGitLabGateway("secret-token-xyz", newTestLogger()).WithBaseURL(srv.URL)
	e.uc = NewSyncUseCase(e.git, gl, state.NewFileStore(e.git), newTestLogger())
	e.init()

	e.gl.edit(map[string]string{"dir/sub/b.txt": "b\nremote\n", "a.txt": ""})
	e.write("dir/new file.txt", "spaces & ünïcode")
	e.write("keep.txt", "k-local")
	e.commit("local")

	res, err := e.uc.Sync(context.Background(), SyncInput{RepoPath: e.repo})
	require.NoError(t, err)
	require.Equal(t, 1, res.Pulled)
	require.Equal(t, 2, res.Pushed)
	e.assertInSync()
}

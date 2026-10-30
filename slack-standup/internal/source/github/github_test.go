package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
)

const username = "kcotten"

var lastWorkday = time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

// newTestClient serves the handlers' routes and points a Client at them.
func newTestClient(t *testing.T, repos []string, routes map[string]http.HandlerFunc) *Client {
	t.Helper()

	mux := http.NewServeMux()
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, handler)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := New(server.Client(), "test-token", username, repos)
	client.BaseURL = server.URL
	return client
}

func commitJSON(sha, message string) map[string]any {
	return map[string]any{
		"sha":      sha,
		"html_url": "https://github.com/acme/api/commit/" + sha,
		"commit":   map[string]any{"message": message},
	}
}

func pullJSON(number int, author, created, updated string, merged *string) map[string]any {
	return map[string]any{
		"number":     number,
		"title":      fmt.Sprintf("PR %d", number),
		"html_url":   fmt.Sprintf("https://github.com/acme/api/pull/%d", number),
		"created_at": created,
		"updated_at": updated,
		"merged_at":  merged,
		"user":       map[string]any{"login": author},
	}
}

func TestActivityCollectsOnlyTheUsersWorkSinceLastWorkday(t *testing.T) {
	mergedFriday := "2025-03-07T15:00:00Z"
	pullsPageTwoFetched := false

	client := newTestClient(t, []string{"acme/api"}, map[string]http.HandlerFunc{
		"/repos/acme/api/branches": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]string{{"name": "main"}, {"name": "feature"}})
		},
		"/repos/acme/api/commits": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("author") != username || query.Get("since") != "2025-03-07T00:00:00Z" {
				t.Errorf("commit query = %v, want author and since filters", query)
			}
			// "shared" is reachable from both branches and must be reported once.
			switch query.Get("sha") {
			case "main":
				writeJSON(t, w, []any{commitJSON("1111111aaaa", "Fix login\n\nLong body"), commitJSON("2222222bbbb", "Shared")})
			case "feature":
				writeJSON(t, w, []any{commitJSON("2222222bbbb", "Shared"), commitJSON("3333333cccc", "Add importer")})
			}
		},
		"/repos/acme/api/pulls": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				pullsPageTwoFetched = true
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/acme/api/pulls?page=2>; rel="next"`, r.Host))
			writeJSON(t, w, []any{
				pullJSON(4, username, "2025-03-10T09:00:00Z", "2025-03-10T09:30:00Z", nil),
				pullJSON(3, "someone-else", "2025-03-10T08:00:00Z", "2025-03-10T08:00:00Z", nil),
				pullJSON(2, username, "2025-03-05T09:00:00Z", "2025-03-07T15:00:00Z", &mergedFriday),
				pullJSON(1, username, "2025-03-01T09:00:00Z", "2025-03-06T23:59:59Z", nil),
			})
		},
		"/search/issues": func(w http.ResponseWriter, r *http.Request) {
			if q := r.URL.Query().Get("q"); !strings.Contains(q, "reviewed-by:"+username) || !strings.Contains(q, "updated:>=2025-03-07") {
				t.Errorf("search q = %q, want reviewer and date filters", q)
			}
			writeJSON(t, w, map[string]any{"items": []map[string]any{{
				"number":         77,
				"title":          "Review me",
				"html_url":       "https://github.com/acme/web/pull/77",
				"repository_url": "https://api.github.com/repos/acme/web",
			}}})
		},
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	wantCommits := []activity.Commit{
		{Repo: "acme/api", Branch: "main", Message: "Fix login", SHA: "1111111", URL: "https://github.com/acme/api/commit/1111111aaaa"},
		{Repo: "acme/api", Branch: "main", Message: "Shared", SHA: "2222222", URL: "https://github.com/acme/api/commit/2222222bbbb"},
		{Repo: "acme/api", Branch: "feature", Message: "Add importer", SHA: "3333333", URL: "https://github.com/acme/api/commit/3333333cccc"},
	}
	if !reflect.DeepEqual(got.Commits, wantCommits) {
		t.Errorf("Commits = %+v, want %+v", got.Commits, wantCommits)
	}

	wantOpened := []activity.PullRequest{{Title: "PR 4", Repo: "acme/api", URL: "https://github.com/acme/api/pull/4", Number: 4}}
	if !reflect.DeepEqual(got.PRsOpened, wantOpened) {
		t.Errorf("PRsOpened = %+v, want %+v", got.PRsOpened, wantOpened)
	}
	wantMerged := []activity.PullRequest{{Title: "PR 2", Repo: "acme/api", URL: "https://github.com/acme/api/pull/2", Number: 2}}
	if !reflect.DeepEqual(got.PRsMerged, wantMerged) {
		t.Errorf("PRsMerged = %+v, want %+v", got.PRsMerged, wantMerged)
	}
	if pullsPageTwoFetched {
		t.Error("kept paging PRs after reaching one last updated before the cut-off")
	}

	wantReviews := []activity.PullRequest{{Title: "Review me", Repo: "acme/web", URL: "https://github.com/acme/web/pull/77", Number: 77}}
	if !reflect.DeepEqual(got.Reviews, wantReviews) {
		t.Errorf("Reviews = %+v, want %+v", got.Reviews, wantReviews)
	}
}

func TestCommitsFollowPagedBranchListings(t *testing.T) {
	client := newTestClient(t, []string{"acme/api"}, map[string]http.HandlerFunc{
		"/repos/acme/api/branches": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				writeJSON(t, w, []map[string]string{{"name": "second-page-branch"}})
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/acme/api/branches?page=2>; rel="next"`, r.Host))
			writeJSON(t, w, []map[string]string{{"name": "main"}})
		},
		"/repos/acme/api/commits": func(w http.ResponseWriter, r *http.Request) {
			sha := "aaaaaaa" + r.URL.Query().Get("sha")
			writeJSON(t, w, []any{commitJSON(sha, "On "+r.URL.Query().Get("sha"))})
		},
	})

	commits, err := client.commits(context.Background(), "acme/api", lastWorkday)

	if err != nil {
		t.Fatalf("commits() error = %v", err)
	}
	var branches []string
	for _, commit := range commits {
		branches = append(branches, commit.Branch)
	}
	if want := []string{"main", "second-page-branch"}; !reflect.DeepEqual(branches, want) {
		t.Errorf("commit branches = %q, want %q", branches, want)
	}
}

func TestActivityKeepsPartialResultsWhenOneRepoFails(t *testing.T) {
	client := newTestClient(t, []string{"acme/broken", "acme/api"}, map[string]http.HandlerFunc{
		"/repos/acme/broken/branches": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		"/repos/acme/broken/pulls": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		"/repos/acme/api/branches": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]string{{"name": "main"}})
		},
		"/repos/acme/api/commits": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []any{commitJSON("1111111aaaa", "Still reported")})
		},
		"/repos/acme/api/pulls": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []any{})
		},
		"/search/issues": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, map[string]any{"items": []any{}})
		},
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if len(got.Commits) != 1 || got.Commits[0].Message != "Still reported" {
		t.Errorf("Commits = %+v, want the healthy repo's commit", got.Commits)
	}
	if err == nil || !strings.Contains(err.Error(), "branches for acme/broken") || !strings.Contains(err.Error(), "PRs for acme/broken") {
		t.Errorf("error = %v, want it to name each failed call for acme/broken", err)
	}
}

func TestActivityRequiresAToken(t *testing.T) {
	client := New(http.DefaultClient, "", username, []string{"acme/api"})

	_, err := client.Activity(context.Background(), lastWorkday)

	if !errors.Is(err, httpx.ErrMissingToken) {
		t.Errorf("error = %v, want httpx.ErrMissingToken", err)
	}
}

func TestRepoFromAPIURL(t *testing.T) {
	got := repoFromAPIURL("https://api.github.com/repos/acme/web")
	if got != "acme/web" {
		t.Errorf("repoFromAPIURL() = %q, want %q", got, "acme/web")
	}
}

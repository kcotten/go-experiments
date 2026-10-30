package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNextLinkFindsTheNextPage(t *testing.T) {
	header := `<https://api.example/items?page=1>; rel="prev", <https://api.example/items?page=3>; rel="next", <https://api.example/items?page=9>; rel="last"`

	if got, want := nextLink(header), "https://api.example/items?page=3"; got != want {
		t.Errorf("nextLink() = %q, want %q", got, want)
	}
}

func TestNextLinkIsEmptyOnTheLastPage(t *testing.T) {
	if got := nextLink(`<https://api.example/items?page=1>; rel="prev"`); got != "" {
		t.Errorf("nextLink() = %q, want empty", got)
	}
	if got := nextLink(""); got != "" {
		t.Errorf("nextLink(\"\") = %q, want empty", got)
	}
}

func TestGetJSONReportsStatusAndStartOfBodyOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusForbidden)
	}))
	defer server.Close()

	var out []string
	_, err := GetJSON(context.Background(), server.Client(), server.URL, nil, &out)

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want a *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusForbidden || !strings.Contains(statusErr.Body, "rate limit exceeded") {
		t.Errorf("StatusError = %+v, want 403 with the response body", statusErr)
	}
}

func TestEachPageFollowsLinksUntilHandlerStops(t *testing.T) {
	requested := map[string]bool{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		requested[page] = true
		w.Header().Set("Link", fmt.Sprintf(`<%s/?page=%s>; rel="next"`, server.URL, page+"x"))
		fmt.Fprintf(w, `["item-%s"]`, page)
	}))
	defer server.Close()

	var seen []string
	err := EachPage(context.Background(), server.Client(), server.URL+"/?page=1", nil,
		func(items []string) bool {
			seen = append(seen, items...)
			return len(seen) < 2
		})

	if err != nil {
		t.Fatalf("EachPage() error = %v", err)
	}
	if len(seen) != 2 {
		t.Errorf("seen = %q, want exactly two pages", seen)
	}
	if requested["1xx"] {
		t.Error("fetched a third page after the handler asked to stop")
	}
}

func TestEachPageFailsLoudlyRatherThanTruncatingSilently(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/>; rel="next"`, server.URL))
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	err := EachPage(context.Background(), server.Client(), server.URL, nil,
		func([]string) bool { return true })

	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("EachPage() error = %v, want an 'incomplete results' error", err)
	}
}

func TestLoggingClientRecordsRequestsButNeverHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := NewLoggingClient(logger)

	var out []string
	header := http.Header{"Authorization": {"token super-secret"}}
	if _, err := GetJSON(context.Background(), client, server.URL+"/repos/acme/api/branches?per_page=100", header, &out); err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}

	logged := logs.String()
	for _, want := range []string{"method=GET", "/repos/acme/api/branches?per_page=100", "status=200", "duration="} {
		if !strings.Contains(logged, want) {
			t.Errorf("log missing %q:\n%s", want, logged)
		}
	}
	if strings.Contains(logged, "super-secret") {
		t.Errorf("log leaks the Authorization header:\n%s", logged)
	}
}

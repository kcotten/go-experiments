package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
)

var lastWorkday = time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)

func pageJSON(title, space, webui string) map[string]any {
	return map[string]any{
		"title":  title,
		"space":  map[string]any{"name": space},
		"_links": map[string]any{"webui": webui},
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := New(server.Client(), server.URL, "kris@example.com", "secret")
	return client
}

func TestActivitySeparatesCreatedFromUpdatedPages(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			t.Error("request had no basic auth")
		}
		cql := r.URL.Query().Get("cql")
		if !strings.Contains(cql, `"2025-03-07"`) {
			t.Errorf("cql = %q, want the last workday's date", cql)
		}

		switch {
		case strings.HasPrefix(cql, "creator = currentUser()"):
			json.NewEncoder(w).Encode(map[string]any{"results": []any{
				pageJSON("Design doc", "Engineering", "/spaces/ENG/pages/1"),
			}})
		case strings.HasPrefix(cql, "contributor = currentUser()"):
			json.NewEncoder(w).Encode(map[string]any{"results": []any{
				pageJSON("Runbook", "Ops", "/spaces/OPS/pages/2"),
			}})
		default:
			t.Errorf("unexpected cql %q", cql)
		}
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	wantCreated := []activity.Page{{Title: "Design doc", Space: "Engineering", URL: client.BaseURL + "/wiki/spaces/ENG/pages/1"}}
	if !reflect.DeepEqual(got.Created, wantCreated) {
		t.Errorf("Created = %+v, want %+v", got.Created, wantCreated)
	}
	wantUpdated := []activity.Page{{Title: "Runbook", Space: "Ops", URL: client.BaseURL + "/wiki/spaces/OPS/pages/2"}}
	if !reflect.DeepEqual(got.Updated, wantUpdated) {
		t.Errorf("Updated = %+v, want %+v", got.Updated, wantUpdated)
	}
}

func TestActivityFollowsRelativeNextLinks(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Query().Get("cql"), "creator") {
			json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			return
		}
		if r.URL.Query().Get("start") == "50" {
			json.NewEncoder(w).Encode(map[string]any{"results": []any{pageJSON("Second", "ENG", "/p/2")}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{pageJSON("First", "ENG", "/p/1")},
			"_links":  map[string]any{"next": "/rest/api/content/search?cql=" + url.QueryEscape(r.URL.Query().Get("cql")) + "&start=50"},
		})
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if len(got.Created) != 2 || got.Created[1].Title != "Second" {
		t.Errorf("Created = %+v, want both pages", got.Created)
	}
}

func TestActivityKeepsCreatedPagesWhenTheUpdatedSearchFails(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Query().Get("cql"), "creator") {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{pageJSON("Kept", "ENG", "/p/1")}})
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if len(got.Created) != 1 {
		t.Errorf("Created = %+v, want the successful search's page", got.Created)
	}
	if err == nil || !strings.Contains(err.Error(), "Confluence updated pages") {
		t.Errorf("error = %v, want it to name the failed search", err)
	}
}

func TestActivityRequiresAToken(t *testing.T) {
	client := New(http.DefaultClient, "https://example.atlassian.net", "kris@example.com", "")

	_, err := client.Activity(context.Background(), lastWorkday)

	if !errors.Is(err, httpx.ErrMissingToken) {
		t.Errorf("error = %v, want httpx.ErrMissingToken", err)
	}
}

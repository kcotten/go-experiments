package jira

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
)

var lastWorkday = time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)

func issueJSON(key, status string) map[string]any {
	return map[string]any{
		"key": key,
		"fields": map[string]any{
			"summary": "Summary of " + key,
			"status":  map[string]any{"name": status},
		},
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := New(server.Client(), server.URL, "kris@example.com", "secret")
	return client
}

func TestActivityGroupsTicketsByProgress(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		username, token, ok := r.BasicAuth()
		if !ok || username != "kris@example.com" || token != "secret" {
			t.Errorf("BasicAuth() = %q, %q, %v, want the configured credentials", username, token, ok)
		}
		jql := r.URL.Query().Get("jql")
		if !strings.Contains(jql, `updated >= "2025-03-07"`) || !strings.Contains(jql, "assignee = currentUser()") {
			t.Errorf("jql = %q, want assignee and date filters", jql)
		}

		json.NewEncoder(w).Encode(map[string]any{"issues": []any{
			issueJSON("PROJ-1", "Done"),
			issueJSON("PROJ-2", "Closed"),
			issueJSON("PROJ-3", "In Progress"),
			issueJSON("PROJ-4", "Code Review"),
			issueJSON("PROJ-5", "To Do"),
		}})
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	keys := func(tickets []activity.Ticket) []string {
		var result []string
		for _, ticket := range tickets {
			result = append(result, ticket.Key)
		}
		return result
	}
	if want := []string{"PROJ-1", "PROJ-2"}; !reflect.DeepEqual(keys(got.Completed), want) {
		t.Errorf("Completed = %q, want %q", keys(got.Completed), want)
	}
	if want := []string{"PROJ-3", "PROJ-4"}; !reflect.DeepEqual(keys(got.InProgress), want) {
		t.Errorf("InProgress = %q, want %q", keys(got.InProgress), want)
	}
	if want := []string{"PROJ-5"}; !reflect.DeepEqual(keys(got.Updated), want) {
		t.Errorf("Updated = %q, want %q", keys(got.Updated), want)
	}

	wantTicket := activity.Ticket{
		Key:     "PROJ-5",
		Summary: "Summary of PROJ-5",
		Status:  "To Do",
		URL:     client.BaseURL + "/browse/PROJ-5",
	}
	if got.Updated[0] != wantTicket {
		t.Errorf("Updated[0] = %+v, want %+v", got.Updated[0], wantTicket)
	}
}

func TestActivityFollowsNextPageTokens(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("nextPageToken") == "page-2" {
			json.NewEncoder(w).Encode(map[string]any{"issues": []any{issueJSON("PROJ-2", "Done")}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"issues":        []any{issueJSON("PROJ-1", "Done")},
			"nextPageToken": "page-2",
		})
	})

	got, err := client.Activity(context.Background(), lastWorkday)

	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if len(got.Completed) != 2 {
		t.Errorf("Completed = %+v, want tickets from both pages", got.Completed)
	}
}

func TestActivityReportsRejectedCredentials(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorised", http.StatusUnauthorized)
	})

	_, err := client.Activity(context.Background(), lastWorkday)

	var statusErr *httpx.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("error = %v, want a 401 StatusError", err)
	}
}

func TestActivityRequiresAToken(t *testing.T) {
	client := New(http.DefaultClient, "https://example.atlassian.net", "kris@example.com", "")

	_, err := client.Activity(context.Background(), lastWorkday)

	if !errors.Is(err, httpx.ErrMissingToken) {
		t.Errorf("error = %v, want httpx.ErrMissingToken", err)
	}
}

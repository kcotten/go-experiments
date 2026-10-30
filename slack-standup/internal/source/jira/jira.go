// Package jira fetches the tickets assigned to the current user that changed
// recently, grouped by how far along they are.
package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
	"standup/internal/workday"
)

const pageSize = "50"

type Client struct {
	HTTP     *http.Client
	BaseURL  string
	Username string
	Token    string
}

func New(client *http.Client, baseURL, username, token string) *Client {
	return &Client{
		HTTP:     client,
		BaseURL:  baseURL,
		Username: username,
		Token:    token,
	}
}

type searchResponse struct {
	Issues []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"fields"`
	} `json:"issues"`
	NextPageToken string `json:"nextPageToken"`
}

// Activity returns the user's tickets updated since lastWorkday.
//
// Tickets fetched before a failure are returned alongside the error.
func (c *Client) Activity(ctx context.Context, lastWorkday time.Time) (activity.Jira, error) {
	if c.Token == "" {
		return activity.Jira{}, fmt.Errorf("Jira: %w", httpx.ErrMissingToken)
	}

	header := http.Header{
		"Authorization": {httpx.BasicAuth(c.Username, c.Token)},
		"Accept":        {"application/json"},
	}
	jql := fmt.Sprintf(
		`assignee = currentUser() AND updated >= "%s" ORDER BY updated DESC`,
		lastWorkday.Format(workday.DateLayout),
	)

	var result activity.Jira
	pageToken := ""

	for pagesFetched := 0; ; pagesFetched++ {
		if pagesFetched == httpx.MaxPages {
			return result, fmt.Errorf("Jira: stopped after %d pages; results are incomplete", httpx.MaxPages)
		}

		query := url.Values{
			"jql":        {jql},
			"fields":     {"key,summary,status,updated"},
			"maxResults": {pageSize},
		}
		if pageToken != "" {
			query.Set("nextPageToken", pageToken)
		}

		var page searchResponse
		endpoint := c.BaseURL + "/rest/api/3/search/jql?" + query.Encode()
		if _, err := httpx.GetJSON(ctx, c.HTTP, endpoint, header, &page); err != nil {
			return result, fmt.Errorf("Jira tickets: %w", err)
		}

		for _, issue := range page.Issues {
			ticket := activity.Ticket{
				Key:     issue.Key,
				Summary: issue.Fields.Summary,
				Status:  issue.Fields.Status.Name,
				URL:     c.BaseURL + "/browse/" + issue.Key,
			}
			switch classify(ticket.Status) {
			case completed:
				result.Completed = append(result.Completed, ticket)
			case inProgress:
				result.InProgress = append(result.InProgress, ticket)
			default:
				result.Updated = append(result.Updated, ticket)
			}
		}

		if page.NextPageToken == "" {
			return result, nil
		}
		pageToken = page.NextPageToken
	}
}

type stage int

const (
	other stage = iota
	completed
	inProgress
)

// classify buckets a workflow status by keyword, since status names vary by project.
func classify(status string) stage {
	lowered := strings.ToLower(status)
	switch {
	case containsAny(lowered, "done", "resolved", "closed"):
		return completed
	case containsAny(lowered, "progress", "review"):
		return inProgress
	default:
		return other
	}
}

func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

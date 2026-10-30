// Package confluence fetches the pages the current user created or edited
// recently.
//
// Activity returns whatever it managed to collect together with a joined
// error describing any search that failed.
package confluence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
	"standup/internal/workday"
)

const (
	wikiPath = "/wiki"
	pageSize = "50"
)

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
	Results []struct {
		Title string `json:"title"`
		Space struct {
			Name string `json:"name"`
		} `json:"space"`
		Links struct {
			WebUI string `json:"webui"`
		} `json:"_links"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// Activity returns pages created, and pages contributed to, since lastWorkday.
func (c *Client) Activity(ctx context.Context, lastWorkday time.Time) (activity.Confluence, error) {
	if c.Token == "" {
		return activity.Confluence{}, fmt.Errorf("Confluence: %w", httpx.ErrMissingToken)
	}

	date := lastWorkday.Format(workday.DateLayout)
	var result activity.Confluence
	var errs []error

	var err error
	result.Created, err = c.search(ctx, fmt.Sprintf(`creator = currentUser() AND created >= "%s" AND type = "page"`, date))
	errs = append(errs, wrap("created pages", err))

	result.Updated, err = c.search(ctx, fmt.Sprintf(`contributor = currentUser() AND lastModified >= "%s" AND type = "page"`, date))
	errs = append(errs, wrap("updated pages", err))

	return result, errors.Join(errs...)
}

// search runs a CQL query and follows the pagination links in the response.
func (c *Client) search(ctx context.Context, cql string) ([]activity.Page, error) {
	header := http.Header{
		"Authorization": {httpx.BasicAuth(c.Username, c.Token)},
		"Accept":        {"application/json"},
	}
	query := url.Values{
		"cql":    {cql},
		"expand": {"space"},
		"limit":  {pageSize},
	}

	var pages []activity.Page
	next := c.BaseURL + wikiPath + "/rest/api/content/search?" + query.Encode()

	for pagesFetched := 0; next != ""; pagesFetched++ {
		if pagesFetched == httpx.MaxPages {
			return pages, fmt.Errorf("stopped after %d pages; results are incomplete", httpx.MaxPages)
		}

		var response searchResponse
		if _, err := httpx.GetJSON(ctx, c.HTTP, next, header, &response); err != nil {
			return pages, err
		}

		for _, result := range response.Results {
			pages = append(pages, activity.Page{
				Title: result.Title,
				Space: result.Space.Name,
				URL:   c.BaseURL + wikiPath + result.Links.WebUI,
			})
		}

		next = ""
		if response.Links.Next != "" {
			// The next link is relative to the wiki root.
			next = c.BaseURL + wikiPath + response.Links.Next
		}
	}
	return pages, nil
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("Confluence %s: %w", what, err)
}

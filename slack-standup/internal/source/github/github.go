// Package github fetches a user's recent commits, pull requests and reviews.
//
// Activity returns whatever it managed to collect together with a joined
// error describing anything that failed, so one broken repository does not
// hide the rest.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"standup/internal/activity"
	"standup/internal/httpx"
	"standup/internal/workday"
)

const (
	defaultBaseURL = "https://api.github.com"
	perPage        = "100"
	reviewsPerPage = "50"
	shortSHALength = 7
)

type Client struct {
	HTTP     *http.Client
	BaseURL  string
	Token    string
	Username string
	Repos    []string
}

func New(client *http.Client, token, username string, repos []string) *Client {
	return &Client{
		HTTP:     client,
		BaseURL:  defaultBaseURL,
		Token:    token,
		Username: username,
		Repos:    repos,
	}
}

// Activity collects the user's work since the start of lastWorkday (UTC).
func (c *Client) Activity(ctx context.Context, lastWorkday time.Time) (activity.GitHub, error) {
	if c.Token == "" {
		return activity.GitHub{}, fmt.Errorf("GitHub: %w", httpx.ErrMissingToken)
	}

	since := workday.StartOfDayUTC(lastWorkday)
	var result activity.GitHub
	var errs []error

	for _, repo := range c.Repos {
		commits, err := c.commits(ctx, repo, since)
		result.Commits = append(result.Commits, commits...)
		errs = append(errs, err)
	}

	for _, repo := range c.Repos {
		opened, merged, err := c.pullRequests(ctx, repo, since)
		result.PRsOpened = append(result.PRsOpened, opened...)
		result.PRsMerged = append(result.PRsMerged, merged...)
		errs = append(errs, err)
	}

	reviews, err := c.reviews(ctx, since)
	result.Reviews = reviews
	errs = append(errs, err)

	return result, errors.Join(errs...)
}

func (c *Client) header() http.Header {
	return http.Header{
		"Authorization": {"token " + c.Token},
		"Accept":        {"application/vnd.github.v3+json"},
	}
}

func (c *Client) endpoint(path string, query url.Values) string {
	return c.BaseURL + path + "?" + query.Encode()
}

type branchResponse struct {
	Name string `json:"name"`
}

// branches lists every branch name; the commits API only covers the default
// branch unless told otherwise.
func (c *Client) branches(ctx context.Context, repo string) ([]string, error) {
	var names []string
	endpoint := c.endpoint("/repos/"+repo+"/branches", url.Values{"per_page": {perPage}})

	err := httpx.EachPage(ctx, c.HTTP, endpoint, c.header(), func(page []branchResponse) bool {
		for _, branch := range page {
			names = append(names, branch.Name)
		}
		return true
	})
	if err != nil {
		return names, fmt.Errorf("branches for %s: %w", repo, err)
	}
	return names, nil
}

type commitResponse struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
	} `json:"commit"`
}

// commits returns the user's commits across all branches of one repo,
// de-duplicated by SHA.
func (c *Client) commits(ctx context.Context, repo string, since time.Time) ([]activity.Commit, error) {
	branches, err := c.branches(ctx, repo)
	errs := []error{err}

	var commits []activity.Commit
	seen := make(map[string]bool)

	for _, branch := range branches {
		query := url.Values{
			"author":   {c.Username},
			"since":    {since.Format(time.RFC3339)},
			"sha":      {branch},
			"per_page": {perPage},
		}
		endpoint := c.endpoint("/repos/"+repo+"/commits", query)

		err := httpx.EachPage(ctx, c.HTTP, endpoint, c.header(), func(page []commitResponse) bool {
			for _, commit := range page {
				if seen[commit.SHA] {
					continue
				}
				seen[commit.SHA] = true
				commits = append(commits, activity.Commit{
					Repo:    repo,
					Branch:  branch,
					Message: firstLine(commit.Commit.Message),
					SHA:     shortSHA(commit.SHA),
					URL:     commit.HTMLURL,
				})
			}
			return true
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("commits for %s@%s: %w", repo, branch, err))
		}
	}

	return commits, errors.Join(errs...)
}

type pullRequestResponse struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// pullRequests returns the user's PRs opened and merged since the given time.
//
// PRs are listed most recently updated first. Anything opened or merged since
// then has been updated since then, so paging can stop at the first PR that
// was not.
func (c *Client) pullRequests(
	ctx context.Context,
	repo string,
	since time.Time,
) (opened, merged []activity.PullRequest, err error) {
	query := url.Values{
		"state":     {"all"},
		"sort":      {"updated"},
		"direction": {"desc"},
		"per_page":  {perPage},
	}
	endpoint := c.endpoint("/repos/"+repo+"/pulls", query)

	err = httpx.EachPage(ctx, c.HTTP, endpoint, c.header(), func(page []pullRequestResponse) bool {
		for _, pr := range page {
			if pr.UpdatedAt.Before(since) {
				return false
			}
			if pr.User.Login != c.Username {
				continue
			}

			summary := activity.PullRequest{
				Title:  pr.Title,
				Repo:   repo,
				URL:    pr.HTMLURL,
				Number: pr.Number,
			}
			if !pr.CreatedAt.Before(since) {
				opened = append(opened, summary)
			}
			if pr.MergedAt != nil && !pr.MergedAt.Before(since) {
				merged = append(merged, summary)
			}
		}
		return true
	})
	if err != nil {
		err = fmt.Errorf("PRs for %s: %w", repo, err)
	}
	return opened, merged, err
}

type reviewSearchResponse struct {
	Items []struct {
		Number        int    `json:"number"`
		Title         string `json:"title"`
		HTMLURL       string `json:"html_url"`
		RepositoryURL string `json:"repository_url"`
	} `json:"items"`
}

// reviews finds PRs the user reviewed using the search API, which is the only
// way to look reviews up by reviewer. Only the first page of results is used.
func (c *Client) reviews(ctx context.Context, since time.Time) ([]activity.PullRequest, error) {
	query := url.Values{
		"q":        {fmt.Sprintf("type:pr reviewed-by:%s updated:>=%s", c.Username, since.Format(workday.DateLayout))},
		"sort":     {"updated"},
		"order":    {"desc"},
		"per_page": {reviewsPerPage},
	}

	var response reviewSearchResponse
	if _, err := httpx.GetJSON(ctx, c.HTTP, c.endpoint("/search/issues", query), c.header(), &response); err != nil {
		return nil, fmt.Errorf("PR reviews: %w", err)
	}

	reviews := make([]activity.PullRequest, len(response.Items))
	for i, item := range response.Items {
		reviews[i] = activity.PullRequest{
			Title:  item.Title,
			Repo:   repoFromAPIURL(item.RepositoryURL),
			URL:    item.HTMLURL,
			Number: item.Number,
		}
	}
	return reviews, nil
}

// repoFromAPIURL turns "https://api.github.com/repos/org/repo" into "org/repo".
func repoFromAPIURL(repositoryURL string) string {
	parts := strings.Split(repositoryURL, "/")
	if len(parts) < 2 {
		return repositoryURL
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return line
}

func shortSHA(sha string) string {
	if len(sha) <= shortSHALength {
		return sha
	}
	return sha[:shortSHALength]
}

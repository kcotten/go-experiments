// Package activity holds the data each source reports for a standup.
//
// The JSON tags are the keys used by the test fixtures in
// internal/render/testdata.
package activity

type Commit struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	SHA     string `json:"sha"`
	URL     string `json:"url"`
}

// PullRequest is used for opened, merged and reviewed PRs alike.
type PullRequest struct {
	Title  string `json:"title"`
	Repo   string `json:"repo"`
	URL    string `json:"url"`
	Number int    `json:"number"`
}

type GitHub struct {
	Commits   []Commit      `json:"commits"`
	PRsOpened []PullRequest `json:"prs_opened"`
	PRsMerged []PullRequest `json:"prs_merged"`
	Reviews   []PullRequest `json:"reviews"`
}

type Ticket struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
	URL     string `json:"url"`
}

type Jira struct {
	Completed  []Ticket `json:"completed"`
	Updated    []Ticket `json:"updated"`
	InProgress []Ticket `json:"in_progress"`
}

type Page struct {
	Title string `json:"title"`
	Space string `json:"space"`
	URL   string `json:"url"`
}

type Confluence struct {
	Created []Page `json:"created"`
	Updated []Page `json:"updated"`
}

// Notes are the bullet points read from the personal notes file.
type Notes struct {
	Completed []string `json:"completed"`
	Plan      []string `json:"plan"`
}

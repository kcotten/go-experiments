// Package httpx holds the small HTTP helpers shared by the API clients.
package httpx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	maxErrorBodyBytes = 200

	// MaxPages bounds how many pages one listing may follow.
	MaxPages = 20
)

// ErrMissingToken is returned by a client that has no API token configured.
var ErrMissingToken = errors.New("API token not set")

// NewClient returns an HTTP client with a request timeout.
func NewClient() *http.Client {
	return &http.Client{Timeout: defaultTimeout}
}

// BasicAuth returns an Authorization header value for username:token.
func BasicAuth(username, token string) string {
	credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	return "Basic " + credentials
}

// StatusError reports a response with an unexpected status code.
type StatusError struct {
	Method     string
	URL        string
	StatusCode int
	// Body is the start of the response body, for diagnosis.
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// GetJSON fetches rawURL and decodes the JSON response into out. It returns
// the URL of the next page from the Link header, or "" when there is none.
func GetJSON(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	header http.Header,
	out any,
) (nextURL string, err error) {
	return doJSON(ctx, client, http.MethodGet, rawURL, header, nil, out)
}

// PostJSON sends body as JSON to rawURL and decodes the JSON response into out.
func PostJSON(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	header http.Header,
	body any,
	out any,
) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding request body for %s: %w", rawURL, err)
	}

	postHeader := http.Header{"Content-Type": {"application/json"}}
	for name, values := range header {
		postHeader[name] = values
	}

	_, err = doJSON(ctx, client, http.MethodPost, rawURL, postHeader, bytes.NewReader(encoded), out)
	return err
}

func doJSON(
	ctx context.Context,
	client *http.Client,
	method string,
	rawURL string,
	header http.Header,
	body io.Reader,
	out any,
) (nextURL string, err error) {
	request, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return "", fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	for name, values := range header {
		request.Header[name] = values
	}

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, request.URL.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
		return "", &StatusError{
			Method:     method,
			URL:        request.URL.Redacted(),
			StatusCode: response.StatusCode,
			Body:       strings.TrimSpace(string(errorBody)),
		}
	}

	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return "", fmt.Errorf("decoding response from %s: %w", request.URL.Redacted(), err)
	}
	return nextLink(response.Header.Get("Link")), nil
}

// EachPage fetches a JSON array endpoint and follows its Link headers,
// passing each page to handle until it returns false or the pages run out.
func EachPage[T any](
	ctx context.Context,
	client *http.Client,
	startURL string,
	header http.Header,
	handle func(items []T) (more bool),
) error {
	next := startURL
	for pagesFetched := 0; next != ""; pagesFetched++ {
		if pagesFetched == MaxPages {
			return fmt.Errorf("stopped after %d pages of %s; results are incomplete", MaxPages, startURL)
		}

		var items []T
		var err error
		next, err = GetJSON(ctx, client, next, header, &items)
		if err != nil {
			return err
		}
		if !handle(items) {
			return nil
		}
	}
	return nil
}

// nextLink extracts the rel="next" URL from a Link header value such as
// `<https://api.example/items?page=2>; rel="next", <...>; rel="last"`.
func nextLink(linkHeader string) string {
	for _, link := range strings.Split(linkHeader, ",") {
		target, params, found := strings.Cut(link, ";")
		if !found || !strings.Contains(params, `rel="next"`) {
			continue
		}
		return strings.Trim(strings.TrimSpace(target), "<>")
	}
	return ""
}

// NewLoggingClient returns a client like NewClient that logs every request at
// debug level: method, URL, status and duration. Headers and bodies are never
// logged, but URLs are, so do not use it for URLs that contain secrets.
func NewLoggingClient(logger *slog.Logger) *http.Client {
	client := NewClient()
	client.Transport = loggingTransport{base: http.DefaultTransport, logger: logger}
	return client
}

type loggingTransport struct {
	base   http.RoundTripper
	logger *slog.Logger
}

func (t loggingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	response, err := t.base.RoundTrip(request)

	attributes := []any{
		"method", request.Method,
		"url", request.URL.Redacted(),
		"duration", time.Since(started).Round(time.Millisecond),
	}
	if err != nil {
		t.logger.Debug("HTTP request failed", append(attributes, "error", err)...)
		return response, err
	}
	t.logger.Debug("HTTP request", append(attributes, "status", response.StatusCode)...)
	return response, nil
}

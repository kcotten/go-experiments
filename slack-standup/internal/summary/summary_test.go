package summary

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"standup/internal/activity"
	"standup/internal/config"
)

// Fixtures are shared with the render tests. The prompt and summary golden
// files live beside them.
const fixtureDir = "../render/testdata"

// Setting UPDATE_GOLDEN rewrites the golden files from the current output
// instead of comparing against them. Review the diff before keeping it.
const updateGoldenEnv = "UPDATE_GOLDEN"

func updatingGolden() bool {
	return os.Getenv(updateGoldenEnv) != ""
}

type fixture struct {
	GitHub     *activity.GitHub     `json:"github"`
	Jira       *activity.Jira       `json:"jira"`
	Confluence *activity.Confluence `json:"confluence"`
	Notes      activity.Notes       `json:"notes"`
}

func (f fixture) data() Data {
	var data Data
	if f.GitHub != nil {
		data.GitHub = *f.GitHub
	}
	if f.Jira != nil {
		data.Jira = *f.Jira
	}
	if f.Confluence != nil {
		data.Confluence = *f.Confluence
	}
	data.Notes = f.Notes.Completed
	return data
}

func loadFixtures(t *testing.T) map[string]Data {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(fixtureDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}

	fixtures := make(map[string]Data)
	for _, path := range paths {
		if strings.HasSuffix(path, ".golden.blocks.json") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var f fixture
		if err := json.Unmarshal(content, &f); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
		fixtures[strings.TrimSuffix(filepath.Base(path), ".json")] = f.data()
	}
	if len(fixtures) == 0 {
		t.Fatalf("no fixtures found in %s", fixtureDir)
	}
	return fixtures
}

// checkGolden compares got with the golden file for a fixture, or rewrites the
// file when updating.
func checkGolden(t *testing.T, name, suffix, got string) {
	t.Helper()

	path := filepath.Join(fixtureDir, name+".golden."+suffix)
	if updatingGolden() {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestPromptsMatchGoldenFiles(t *testing.T) {
	for name, data := range loadFixtures(t) {
		for _, style := range []string{"professional", "casual", "detailed"} {
			t.Run(name+"/"+style, func(t *testing.T) {
				checkGolden(t, name, "prompt-"+style+".txt", buildPrompt(data, style))
			})
		}
	}
}

func TestSimpleSummaryMatchesGoldenFile(t *testing.T) {
	for name, data := range loadFixtures(t) {
		t.Run(name, func(t *testing.T) {
			checkGolden(t, name, "simple-summary.txt", simpleSummary(data))
		})
	}
}

func TestPromptFallsBackToProfessionalStyleForUnknownStyle(t *testing.T) {
	data := Data{Notes: []string{"Wrote docs"}}

	if got, want := buildPrompt(data, "shouty"), buildPrompt(data, "professional"); got != want {
		t.Errorf("unknown style prompt = %q, want the professional prompt", got)
	}
}

type failingSummariser struct{}

func (failingSummariser) Summarise(context.Context, Data) (string, error) {
	return "", errors.New("provider down")
}

func TestGenerateSkipsTheProviderWhenThereIsNothingToSummarise(t *testing.T) {
	text, err := Generate(context.Background(), failingSummariser{}, Data{})

	if err != nil || text != "No significant activity found to summarize." {
		t.Errorf("Generate() = %q, %v, want the no-activity text and no error", text, err)
	}
}

func TestGenerateFallsBackToCountsWhenTheProviderFails(t *testing.T) {
	data := Data{Notes: []string{"Wrote docs", "Fixed bug"}}

	text, err := Generate(context.Background(), failingSummariser{}, data)

	if text != "Completed: 2 other tasks." {
		t.Errorf("text = %q, want the simple summary", text)
	}
	if err == nil || !strings.Contains(err.Error(), "provider down") {
		t.Errorf("error = %v, want it to explain why the provider was not used", err)
	}
}

func TestForChoosesAProviderFromConfigAndSecrets(t *testing.T) {
	secrets := config.Secrets{OpenAIAPIKey: "sk-openai", AnthropicAPIKey: "sk-anthropic"}
	tests := []struct {
		name         string
		provider     string
		secrets      config.Secrets
		wantType     string
		wantErrorMsg string
	}{
		{"openai with a key", "openai", secrets, "*summary.OpenAI", ""},
		{"anthropic with a key", "anthropic", secrets, "*summary.Anthropic", ""},
		{"none", "none", secrets, "summary.Simple", ""},
		{"unset provider", "", secrets, "summary.Simple", ""},
		{"openai without a key", "openai", config.Secrets{}, "summary.Simple", "OPENAI_API_KEY"},
		{"anthropic without a key", "anthropic", config.Secrets{}, "summary.Simple", "ANTHROPIC_API_KEY"},
		{"unknown provider", "gemini", secrets, "summary.Simple", "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := For(config.AISummary{Provider: test.provider}, test.secrets, http.DefaultClient)

			if gotType := typeName(got); gotType != test.wantType {
				t.Errorf("For() summariser = %s, want %s", gotType, test.wantType)
			}
			if test.wantErrorMsg == "" && err != nil {
				t.Errorf("For() error = %v, want nil", err)
			}
			if test.wantErrorMsg != "" && (err == nil || !strings.Contains(err.Error(), test.wantErrorMsg)) {
				t.Errorf("For() error = %v, want it to mention %q", err, test.wantErrorMsg)
			}
		})
	}
}

func typeName(summariser Summariser) string {
	switch summariser.(type) {
	case *OpenAI:
		return "*summary.OpenAI"
	case *Anthropic:
		return "*summary.Anthropic"
	case Simple:
		return "summary.Simple"
	default:
		return "unknown"
	}
}

func TestOpenAISendsThePromptAndTrimsTheReply(t *testing.T) {
	var gotAuth string
	var gotRequest openAIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  Shipped the fix.\n"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAI(server.Client(), "sk-test", "", "casual")
	provider.BaseURL = server.URL
	data := Data{Notes: []string{"Wrote docs"}}

	text, err := provider.Summarise(context.Background(), data)

	if err != nil || text != "Shipped the fix." {
		t.Fatalf("Summarise() = %q, %v, want the trimmed reply", text, err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want a bearer token", gotAuth)
	}
	if gotRequest.Model != "gpt-4o-mini" || gotRequest.MaxTokens != 300 {
		t.Errorf("request = %+v, want the default model and token limit", gotRequest)
	}
	if len(gotRequest.Messages) != 2 || gotRequest.Messages[1].Content != buildPrompt(data, "casual") {
		t.Errorf("messages = %+v, want a system message then the casual prompt", gotRequest.Messages)
	}
}

func TestAnthropicSendsThePromptAndTrimsTheReply(t *testing.T) {
	var gotHeader http.Header
	var gotRequest anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Write([]byte(`{"content":[{"type":"text","text":"\nFixed login and wrote docs. "}]}`))
	}))
	defer server.Close()

	provider := NewAnthropic(server.Client(), "sk-ant", "claude-test", "detailed")
	provider.BaseURL = server.URL
	data := Data{Notes: []string{"Wrote docs"}}

	text, err := provider.Summarise(context.Background(), data)

	if err != nil || text != "Fixed login and wrote docs." {
		t.Fatalf("Summarise() = %q, %v, want the trimmed reply", text, err)
	}
	if gotHeader.Get("X-Api-Key") != "sk-ant" || gotHeader.Get("Anthropic-Version") != "2023-06-01" {
		t.Errorf("headers = %v, want the API key and version", gotHeader)
	}
	if gotRequest.Model != "claude-test" || len(gotRequest.Messages) != 1 ||
		gotRequest.Messages[0].Content != buildPrompt(data, "detailed") {
		t.Errorf("request = %+v, want the configured model and detailed prompt", gotRequest)
	}
}

func TestProvidersReportAPIFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	openAI := NewOpenAI(server.Client(), "key", "", "")
	openAI.BaseURL = server.URL
	anthropic := NewAnthropic(server.Client(), "key", "", "")
	anthropic.BaseURL = server.URL

	for name, summariser := range map[string]Summariser{"openai": openAI, "anthropic": anthropic} {
		t.Run(name, func(t *testing.T) {
			_, err := summariser.Summarise(context.Background(), Data{Notes: []string{"x"}})

			if err == nil || !strings.Contains(err.Error(), "503") {
				t.Errorf("Summarise() error = %v, want it to include the 503 status", err)
			}
		})
	}
}

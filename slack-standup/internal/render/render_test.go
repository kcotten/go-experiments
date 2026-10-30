package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"standup/internal/activity"
)

// Setting UPDATE_GOLDEN rewrites the golden files in testdata/ from the current
// output instead of comparing against them. Review the diff before keeping it.
const updateGoldenEnv = "UPDATE_GOLDEN"

func updatingGolden() bool {
	return os.Getenv(updateGoldenEnv) != ""
}

func writeGolden(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture is the input for one golden case: testdata/<name>.json.
type fixture struct {
	Now        string               `json:"now"`
	GitHub     *activity.GitHub     `json:"github"`
	Jira       *activity.Jira       `json:"jira"`
	Confluence *activity.Confluence `json:"confluence"`
	Notes      activity.Notes       `json:"notes"`
	AISummary  *string              `json:"ai_summary"`
}

func (f fixture) input(t *testing.T) Input {
	t.Helper()

	now, err := time.Parse("2006-01-02T15:04:05", f.Now)
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", f.Now, err)
	}

	input := Input{
		Now:        now,
		GitHub:     f.GitHub,
		Jira:       f.Jira,
		Confluence: f.Confluence,
		Notes:      f.Notes,
	}
	if f.AISummary != nil {
		input.AISummary = *f.AISummary
	}
	return input
}

func loadFixtures(t *testing.T) map[string]fixture {
	t.Helper()

	paths, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}

	fixtures := make(map[string]fixture)
	for _, path := range paths {
		if strings.HasSuffix(path, ".golden.blocks.json") {
			continue
		}
		var f fixture
		readJSON(t, path, &f)
		fixtures[strings.TrimSuffix(filepath.Base(path), ".json")] = f
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures found in testdata/")
	}
	return fixtures
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
}

func TestTextMatchesGoldenFile(t *testing.T) {
	for name, f := range loadFixtures(t) {
		t.Run(name, func(t *testing.T) {
			path := "testdata/" + name + ".golden.txt"
			got := Text(f.input(t))
			if updatingGolden() {
				writeGolden(t, path, []byte(got))
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("Text() differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}

// encodeBlocks formats blocks for a golden file: indented, with <, > and & left
// as they are so Slack links stay readable.
func encodeBlocks(t *testing.T, blocks []Block) []byte {
	t.Helper()

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(blocks); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestBlocksMatchGoldenFile(t *testing.T) {
	for name, f := range loadFixtures(t) {
		t.Run(name, func(t *testing.T) {
			path := "testdata/" + name + ".golden.blocks.json"
			if updatingGolden() {
				writeGolden(t, path, encodeBlocks(t, Blocks(f.input(t))))
				return
			}

			var want []any
			readJSON(t, path, &want)

			// Round-trip through JSON so key order and types compare like the golden file.
			encoded, err := json.Marshal(Blocks(f.input(t)))
			if err != nil {
				t.Fatal(err)
			}
			var got []any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, want) {
				gotPretty, _ := json.MarshalIndent(got, "", "  ")
				wantPretty, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("Blocks() differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, gotPretty, wantPretty)
			}
		})
	}
}

func TestMondayHeaderSpansTheWeekend(t *testing.T) {
	monday := time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC)

	dateRange, period := Input{Now: monday}.header()

	if want := "Friday, March 07 - Monday, March 10, 2025"; dateRange != want {
		t.Errorf("dateRange = %q, want %q", dateRange, want)
	}
	if period != "Since Friday" {
		t.Errorf("period = %q, want %q", period, "Since Friday")
	}
}

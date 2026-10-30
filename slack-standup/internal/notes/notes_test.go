package notes

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	friday = time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)
	monday = time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
)

func TestParseCollectsCompletedWorkSinceLastWorkday(t *testing.T) {
	content := strings.Join([]string{
		"## 2025-03-06",
		"### Completed",
		"- Too old to report",
		"## 2025-03-07",
		"### Completed",
		"- Shipped the importer",
		"* Fixed flaky test",
		"## 2025-03-10",
		"### Completed",
		"+ Started the migration",
	}, "\n")

	got := Parse(content, friday, monday)

	want := []string{"Shipped the importer", "Fixed flaky test", "Started the migration"}
	if !reflect.DeepEqual(got.Completed, want) {
		t.Errorf("Completed = %q, want %q", got.Completed, want)
	}
}

func TestParseOnlyPlansForToday(t *testing.T) {
	content := strings.Join([]string{
		"## 2025-03-07",
		"### Plan",
		"- Friday's plan, now stale",
		"## 2025-03-10",
		"### Plan",
		"- Review the release PR",
	}, "\n")

	got := Parse(content, friday, monday)

	want := []string{"Review the release PR"}
	if !reflect.DeepEqual(got.Plan, want) {
		t.Errorf("Plan = %q, want %q", got.Plan, want)
	}
}

func TestParseTreatsBulletsWithoutSubsectionAsCompleted(t *testing.T) {
	content := "## 2025-03-07\n- Legacy format item\n"

	got := Parse(content, friday, monday)

	want := []string{"Legacy format item"}
	if !reflect.DeepEqual(got.Completed, want) {
		t.Errorf("Completed = %q, want %q", got.Completed, want)
	}
}

func TestParseResetsSectionOnNewDateHeader(t *testing.T) {
	content := strings.Join([]string{
		"## 2025-03-07",
		"### Plan",
		"- Ignored plan",
		"## 2025-03-10",
		"- Falls back to completed",
	}, "\n")

	got := Parse(content, friday, monday)

	want := []string{"Falls back to completed"}
	if !reflect.DeepEqual(got.Completed, want) {
		t.Errorf("Completed = %q, want %q", got.Completed, want)
	}
}

func TestParseIgnoresNonBulletLinesAndMalformedDates(t *testing.T) {
	content := strings.Join([]string{
		"## 2025-03-10",
		"Just a paragraph",
		"-no space after dash",
		"## Week of 2025-13-45",
		"- Still under the previous valid date",
	}, "\n")

	got := Parse(content, friday, monday)

	want := []string{"Still under the previous valid date"}
	if !reflect.DeepEqual(got.Completed, want) {
		t.Errorf("Completed = %q, want %q", got.Completed, want)
	}
}

func TestLoadReturnsEmptyNotesWhenFileIsMissing(t *testing.T) {
	got, err := Load(t.TempDir()+"/missing.md", friday, monday)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for a missing file", err)
	}
	if len(got.Completed) != 0 || len(got.Plan) != 0 {
		t.Errorf("Load() = %+v, want empty notes", got)
	}
}

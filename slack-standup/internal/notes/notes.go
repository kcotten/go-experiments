// Package notes reads completed work and today's plan from a markdown notes file.
//
// Expected layout:
//
//	## 2025-03-10
//	### Completed
//	- Fixed the login bug
//	### Plan
//	- Review the release PR
//
// Bullets under a date with no "###" subsection count as completed work.
package notes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"standup/internal/activity"
	"standup/internal/workday"
)

type section int

const (
	sectionNone section = iota
	sectionCompleted
	sectionPlan
)

var datePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// Load reads the notes file. A missing file is not an error: it yields empty notes.
func Load(path string, lastWorkday, today time.Time) (activity.Notes, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return activity.Notes{}, nil
	}
	if err != nil {
		return activity.Notes{}, fmt.Errorf("reading notes file %q: %w", path, err)
	}
	return Parse(string(content), lastWorkday, today), nil
}

// Parse collects completed items dated on or after lastWorkday, plus the plan
// for today only.
func Parse(content string, lastWorkday, today time.Time) activity.Notes {
	var result activity.Notes
	var currentDate time.Time
	currentSection := sectionNone
	capturing := false

	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)

		if isDateHeader(line) {
			if parsed, ok := parseHeaderDate(line, lastWorkday.Location()); ok {
				currentDate = parsed
				capturing = !parsed.Before(lastWorkday)
				currentSection = sectionNone
			}
			continue
		}

		if strings.HasPrefix(line, "###") {
			currentSection = sectionFromHeader(line)
			continue
		}

		item, isBullet := bulletText(line)
		if !isBullet || !capturing {
			continue
		}

		switch currentSection {
		case sectionNone, sectionCompleted: // no subsection is the legacy format
			result.Completed = append(result.Completed, item)
		case sectionPlan:
			if sameDay(currentDate, today) {
				result.Plan = append(result.Plan, item)
			}
		}
	}

	return result
}

// isDateHeader is true for "##" lines that carry a YYYY-MM-DD date.
func isDateHeader(line string) bool {
	return strings.HasPrefix(line, "##") && datePattern.MatchString(line)
}

// parseHeaderDate extracts the first valid YYYY-MM-DD word from a header line,
// as midnight in the given location so it compares cleanly with lastWorkday.
func parseHeaderDate(line string, location *time.Location) (time.Time, bool) {
	for _, word := range strings.Fields(line) {
		if len(word) != len(workday.DateLayout) {
			continue
		}
		if parsed, err := time.ParseInLocation(workday.DateLayout, word, location); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func sectionFromHeader(line string) section {
	lowered := strings.ToLower(line)
	switch {
	case strings.Contains(lowered, "completed"):
		return sectionCompleted
	case strings.Contains(lowered, "plan"):
		return sectionPlan
	default:
		return sectionNone
	}
}

// bulletText returns the text of a markdown bullet line.
func bulletText(line string) (string, bool) {
	for _, marker := range []string{"- ", "* ", "+ "} {
		if text, found := strings.CutPrefix(line, marker); found {
			return text, true
		}
	}
	return "", false
}

func sameDay(a, b time.Time) bool {
	aYear, aMonth, aDay := a.Date()
	bYear, bMonth, bDay := b.Date()
	return aYear == bYear && aMonth == bMonth && aDay == bDay
}

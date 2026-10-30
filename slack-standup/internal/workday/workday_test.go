package workday

import (
	"testing"
	"time"
)

func TestLastWorkdayLooksBackOverTheWeekend(t *testing.T) {
	tests := []struct {
		name string
		now  string
		want string
	}{
		{"Monday reports since Friday", "2025-03-10", "2025-03-07"},
		{"Tuesday reports since Monday", "2025-03-11", "2025-03-10"},
		{"Friday reports since Thursday", "2025-03-14", "2025-03-13"},
		{"Saturday reports since Friday", "2025-03-15", "2025-03-14"},
		{"Sunday reports since Friday", "2025-03-16", "2025-03-14"},
		{"Monday across a month boundary", "2025-03-03", "2025-02-28"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now, err := time.ParseInLocation(DateLayout, test.now, time.UTC)
			if err != nil {
				t.Fatalf("bad test date %q: %v", test.now, err)
			}
			now = now.Add(9*time.Hour + 30*time.Minute)

			got := Last(now).Format(DateLayout)
			if got != test.want {
				t.Errorf("Last(%s) = %s, want %s", test.now, got, test.want)
			}
		})
	}
}

func TestStartOfDayUTCKeepsTheCalendarDateWhateverTheLocation(t *testing.T) {
	zone := time.FixedZone("UTC+10", 10*60*60)
	local := time.Date(2025, 3, 7, 0, 0, 0, 0, zone)

	got := StartOfDayUTC(local)

	want := time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("StartOfDayUTC() = %s, want %s", got, want)
	}
}

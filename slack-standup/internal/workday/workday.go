// Package workday works out which day a standup should report from.
package workday

import "time"

const (
	// DateLayout is the YYYY-MM-DD layout used by the APIs, notes and config.
	DateLayout = "2006-01-02"

	daysBackFromMonday = 3 // Monday looks back to Friday
	daysBackFromSunday = 2 // Sunday looks back to Friday
	daysBackOtherwise  = 1
)

// Last returns midnight on the last workday before now, in now's location.
func Last(now time.Time) time.Time {
	var daysBack int
	switch now.Weekday() {
	case time.Monday:
		daysBack = daysBackFromMonday
	case time.Sunday:
		daysBack = daysBackFromSunday
	default:
		daysBack = daysBackOtherwise
	}

	year, month, day := now.Date()
	return time.Date(year, month, day-daysBack, 0, 0, 0, 0, now.Location())
}

// StartOfDayUTC returns midnight UTC on the calendar date of day.
//
// The GitHub and Atlassian queries filter on UTC timestamps built from the
// last workday's date, so the date is kept and the local offset is dropped.
func StartOfDayUTC(day time.Time) time.Time {
	year, month, dayOfMonth := day.Date()
	return time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
}

package usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(s string) time.Time {
	d, _ := time.ParseInLocation(dayLayout, s, time.Local)
	return d
}

var farFuture = day("2100-01-01")

func TestDateSpread_EvenOverWorkdays(t *testing.T) {
	// Mon 2026-09-07 .. Fri 2026-09-18: 10 working days, weekend in between.
	s, err := ParseDateSpread("2026-09-07", "2026-09-18", "10-19", false, 20*time.Minute)
	require.NoError(t, err)
	s.Seed = 42

	dates, err := s.Schedule(23, farFuture)
	require.NoError(t, err)
	require.Len(t, dates, 23)

	perDay := map[string]int{}
	for i, d := range dates {
		if i > 0 {
			assert.True(t, d.After(dates[i-1]), "dates must increase: %s then %s", dates[i-1], d)
		}
		assert.NotEqual(t, time.Saturday, d.Weekday())
		assert.NotEqual(t, time.Sunday, d.Weekday())
		assert.True(t, d.Hour() >= 10 && d.Hour() < 19, "outside working hours: %s", d)
		perDay[d.Format(dayLayout)]++
	}
	assert.Len(t, perDay, 10, "every working day gets commits")
	for d, n := range perDay {
		assert.True(t, n == 2 || n == 3, "%s has %d commits", d, n)
	}
}

func TestDateSpread_FewCommitsAreSpacedOut(t *testing.T) {
	s, err := ParseDateSpread("2026-09-07", "2026-09-18", "", false, 0)
	require.NoError(t, err)
	dates, err := s.Schedule(3, farFuture)
	require.NoError(t, err)
	// 10 working days, 3 commits: first, middle and last day -> days 0, 5, 9.
	assert.Equal(t, []string{"2026-09-07", "2026-09-14", "2026-09-18"},
		[]string{dates[0].Format(dayLayout), dates[1].Format(dayLayout), dates[2].Format(dayLayout)})
	// No jitter: in the middle of the 10-19 window.
	assert.Equal(t, "14:30", dates[0].Format("15:04"))
}

func TestDateSpread_JitterIsBounded(t *testing.T) {
	s, err := ParseDateSpread("2026-09-07", "2026-09-07", "10-19", false, 30*time.Minute)
	require.NoError(t, err)
	for seed := int64(1); seed <= 50; seed++ {
		s.Seed = seed
		dates, err := s.Schedule(3, farFuture)
		require.NoError(t, err)
		// Three 3h slots centred at 11:30, 14:30, 17:30, each shifted at most 30 min.
		for i, centre := range []string{"11:30", "14:30", "17:30"} {
			c, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-07 "+centre, time.Local)
			diff := dates[i].Sub(c)
			assert.True(t, diff >= -30*time.Minute && diff <= 30*time.Minute, "seed %d: %s", seed, dates[i])
		}
	}
}

func TestDateSpread_Validation(t *testing.T) {
	_, err := ParseDateSpread("2026-09-10", "2026-09-01", "", false, 0)
	assert.Error(t, err)
	_, err = ParseDateSpread("2026-09-12", "2026-09-13", "", false, 0) // weekend only
	assert.Error(t, err)
	s, err := ParseDateSpread("2026-09-12", "2026-09-13", "", true, 0)
	require.NoError(t, err)
	dates, err := s.Schedule(2, farFuture)
	require.NoError(t, err)
	assert.Equal(t, time.Saturday, dates[0].Weekday())

	_, err = ParseDateSpread("2026-09-01", "2026-09-10", "19-10", false, 0)
	assert.Error(t, err)
	_, err = ParseDateSpread("2026/09/01", "2026-09-10", "", false, 0)
	assert.Error(t, err)

	s, err = ParseDateSpread("2026-09-01", "2026-09-10", "", false, 0)
	require.NoError(t, err)
	_, err = s.Schedule(5, day("2026-09-05"))
	require.Error(t, err, "an end date in the future is rejected")
	assert.Contains(t, err.Error(), "the end date 2026-09-10 is in the future (today is 2026-09-05)")
}

func TestDateSpread_EndingToday(t *testing.T) {
	// Mon 2026-09-14 .. Wed 2026-09-16, and "now" is Wednesday 12:00.
	s, err := ParseDateSpread("2026-09-14", "2026-09-16", "10-19", false, 20*time.Minute)
	require.NoError(t, err)
	now := day("2026-09-16").Add(12 * time.Hour)

	for seed := int64(1); seed <= 30; seed++ {
		s.Seed = seed
		dates, err := s.Schedule(9, now)
		require.NoError(t, err)
		assert.False(t, dates[8].After(now), "no commit after now: %s", dates[8])
		assert.Equal(t, "2026-09-16", dates[8].Format(dayLayout), "today is still used")
		for i := 1; i < len(dates); i++ {
			assert.True(t, dates[i].After(dates[i-1]))
		}
	}

	// Before the working day starts, today is skipped.
	early := day("2026-09-16").Add(8 * time.Hour)
	dates, err := s.Schedule(4, early)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-15", dates[3].Format(dayLayout))

	// A one-day period that has not started yet has no working time.
	s, err = ParseDateSpread("2026-09-16", "2026-09-16", "10-19", false, 0)
	require.NoError(t, err)
	_, err = s.Schedule(1, early)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start the period earlier")
}

func TestDateSpread_CoversWholePeriod(t *testing.T) {
	// Mon..Fri, 4 commits: Mon, Tue, Thu, Fri.
	s, err := ParseDateSpread("2026-09-14", "2026-09-18", "", false, 0)
	require.NoError(t, err)
	dates, err := s.Schedule(4, farFuture)
	require.NoError(t, err)
	var got []string
	for _, d := range dates {
		got = append(got, d.Format("Mon"))
	}
	assert.Equal(t, []string{"Mon", "Tue", "Thu", "Fri"}, got)

	one, err := s.Schedule(1, farFuture)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-14", one[0].Format(dayLayout))
}

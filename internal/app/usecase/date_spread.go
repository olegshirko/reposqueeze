package usecase

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// DateSpread assigns commit dates evenly over a period: commits are split
// across working days (±1 per day) and spaced evenly within working hours,
// with a small random shift that never changes their order.
type DateSpread struct {
	From, To  time.Time     // first and last day (dates, local time)
	StartHour int           // working window start, e.g. 10
	EndHour   int           // working window end, e.g. 19
	Weekends  bool          // also use Saturdays and Sundays
	Jitter    time.Duration // max random shift of each commit time
	Seed      int64         // random seed; 0 = time based
}

const dayLayout = "2006-01-02"

// ParseDateSpread builds a DateSpread from user input:
// from/to as YYYY-MM-DD, hours as "10-19".
func ParseDateSpread(from, to, hours string, weekends bool, jitter time.Duration) (*DateSpread, error) {
	f, err := time.ParseInLocation(dayLayout, strings.TrimSpace(from), time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid start date %q, expected YYYY-MM-DD", from)
	}
	t, err := time.ParseInLocation(dayLayout, strings.TrimSpace(to), time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid end date %q, expected YYYY-MM-DD", to)
	}
	start, end, err := parseHours(hours)
	if err != nil {
		return nil, err
	}
	if jitter < 0 {
		return nil, fmt.Errorf("jitter must not be negative")
	}
	s := &DateSpread{From: f, To: t, StartHour: start, EndHour: end, Weekends: weekends, Jitter: jitter}
	return s, s.validate()
}

func parseHours(hours string) (int, int, error) {
	hours = strings.TrimSpace(hours)
	if hours == "" {
		return 10, 19, nil
	}
	a, b, ok := strings.Cut(hours, "-")
	start, err1 := strconv.Atoi(strings.TrimSpace(a))
	end, err2 := strconv.Atoi(strings.TrimSpace(b))
	if !ok || err1 != nil || err2 != nil || start < 0 || end > 24 || start >= end {
		return 0, 0, fmt.Errorf("invalid working hours %q, expected e.g. 10-19", hours)
	}
	return start, end, nil
}

func (s *DateSpread) validate() error {
	if s.To.Before(s.From) {
		return fmt.Errorf("the period ends (%s) before it starts (%s)", s.To.Format(dayLayout), s.From.Format(dayLayout))
	}
	if len(s.days()) == 0 {
		return fmt.Errorf("no working days between %s and %s", s.From.Format(dayLayout), s.To.Format(dayLayout))
	}
	return nil
}

// days lists the usable days of the period.
func (s *DateSpread) days() []time.Time {
	var out []time.Time
	for d := s.From; !d.After(s.To); d = d.AddDate(0, 0, 1) {
		if !s.Weekends && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// Schedule returns n increasing commit times. Times later than now are
// rejected, so history never ends in the future.
func (s *DateSpread) Schedule(n int, now time.Time) ([]time.Time, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	days := s.days()
	seed := s.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rnd := rand.New(rand.NewSource(seed))

	// With at least as many commits as days, commit i goes to day
	// floor(i*D/n): every day is used and counts differ by at most one.
	// With fewer commits, they span the whole period: the first on the
	// first day, the last on the last day, the rest evenly in between.
	perDay := make([][]int, len(days))
	for i := 0; i < n; i++ {
		var d int
		switch {
		case n >= len(days):
			d = i * len(days) / n
		case n > 1:
			d = int(math.Round(float64(i*(len(days)-1)) / float64(n-1)))
		}
		perDay[d] = append(perDay[d], i)
	}

	window := time.Duration(s.EndHour-s.StartHour) * time.Hour
	out := make([]time.Time, n)
	for d, idx := range perDay {
		if len(idx) == 0 {
			continue
		}
		start := time.Date(days[d].Year(), days[d].Month(), days[d].Day(), s.StartHour, 0, 0, 0, time.Local)
		slot := window / time.Duration(len(idx))
		// Keep each commit inside its own slot so the order is preserved.
		maxShift := s.Jitter
		if limit := slot/2 - time.Minute; maxShift > limit {
			maxShift = max(limit, 0)
		}
		for j, i := range idx {
			at := start.Add(slot*time.Duration(j) + slot/2).Truncate(time.Second)
			if maxShift > 0 {
				at = at.Add(time.Duration(rnd.Int63n(int64(2*maxShift))) - maxShift).Truncate(time.Second)
			}
			out[i] = at
		}
	}
	if last := out[n-1]; last.After(now) {
		return nil, fmt.Errorf("the period reaches into the future (%s); end it no later than today", last.Format("2006-01-02 15:04"))
	}
	return out, nil
}

// describeSchedule renders the schedule for a dry run.
func describeSchedule(titles []string, dates []time.Time) []string {
	lines := make([]string, len(titles))
	for i := range titles {
		lines[i] = fmt.Sprintf("  %s  %s", dates[i].Format("Mon 2006-01-02 15:04"), titles[i])
	}
	return lines
}

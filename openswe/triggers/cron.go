package triggers

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed 5-field cron expression (minute hour day-of-month month
// day-of-week), evaluated in UTC.
type Cron struct {
	min, hour, dom, mon, dow uint64
	domStar, dowStar         bool
	Spec                     string
}

var cronRanges = [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var dowNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// ParseCron parses "m h dom mon dow" with *, lists, ranges, steps and
// month/weekday names. Day-of-week 7 means Sunday.
func ParseCron(spec string) (Cron, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return Cron{}, fmt.Errorf("triggers: cron %q must have 5 fields (minute hour day-of-month month day-of-week)", spec)
	}
	var masks [5]uint64
	var star [5]bool
	for i, f := range fields {
		m, st, err := parseField(f, cronRanges[i][0], cronRanges[i][1], i)
		if err != nil {
			return Cron{}, fmt.Errorf("triggers: cron %q field %d: %w", spec, i+1, err)
		}
		masks[i], star[i] = m, st
	}
	if masks[4]&(1<<7) != 0 { // 7 = Sunday
		masks[4] |= 1
	}
	return Cron{min: masks[0], hour: masks[1], dom: masks[2], mon: masks[3], dow: masks[4], domStar: star[2], dowStar: star[4], Spec: spec}, nil
}

func parseField(f string, lo, hi, idx int) (mask uint64, star bool, err error) {
	star = strings.HasPrefix(f, "*") // like Vixie cron: a field starting with * is unrestricted for the dom/dow rule
	for _, part := range strings.Split(f, ",") {
		step, hasStep := 1, false
		if base, s, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return 0, false, fmt.Errorf("bad step %q", s)
			}
			step, hasStep, part = n, true, base
		}
		a, b := lo, hi
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			l, r, _ := strings.Cut(part, "-")
			if a, err = atoiName(l, idx); err != nil {
				return 0, false, err
			}
			if b, err = atoiName(r, idx); err != nil {
				return 0, false, err
			}
		default:
			if a, err = atoiName(part, idx); err != nil {
				return 0, false, err
			}
			if !hasStep {
				b = a
			}
		}
		if a < lo || b > hi || a > b {
			return 0, false, fmt.Errorf("%q is outside %d-%d", part, lo, hi)
		}
		for v := a; v <= b; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, star, nil
}

func atoiName(s string, idx int) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	s = strings.ToLower(s)
	if idx == 3 {
		if n, ok := monthNames[s]; ok {
			return n, nil
		}
	}
	if idx == 4 {
		if n, ok := dowNames[s]; ok {
			return n, nil
		}
	}
	return 0, fmt.Errorf("bad value %q", s)
}

// Matches reports whether the minute of t (UTC) is selected. When both
// day-of-month and day-of-week are restricted, either may match (standard cron).
func (c Cron) Matches(t time.Time) bool {
	t = t.UTC()
	if c.min&(1<<uint(t.Minute())) == 0 || c.hour&(1<<uint(t.Hour())) == 0 || c.mon&(1<<uint(t.Month())) == 0 {
		return false
	}
	dom := c.dom&(1<<uint(t.Day())) != 0
	dow := c.dow&(1<<uint(t.Weekday())) != 0
	switch {
	case c.domStar && c.dowStar:
		return true
	case c.domStar:
		return dow
	case c.dowStar:
		return dom
	}
	return dom || dow
}

// Next returns the first matching minute strictly after t, within 5 years.
func (c Cron) Next(after time.Time) (time.Time, bool) {
	t := after.UTC().Truncate(time.Minute).Add(time.Minute)
	for end := t.AddDate(5, 0, 0); t.Before(end); t = t.Add(time.Minute) {
		if c.Matches(t) {
			return t, true
		}
	}
	return time.Time{}, false
}

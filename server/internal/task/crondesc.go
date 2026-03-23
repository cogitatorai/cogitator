package task

import (
	"fmt"
	"strings"
	"time"
)

// isSimpleCronField returns true when a cron field is a single numeric value
// (no ranges, lists, or steps).
func isSimpleCronField(f string) bool {
	for _, c := range f {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(f) > 0
}

// DescribeCron returns a human-readable description of a 5-field cron expression.
// It handles common patterns; for complex expressions it falls back to "custom schedule".
func DescribeCron(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}

	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]
	simpleTime := isSimpleCronField(minute) && isSimpleCronField(hour)

	// Every N minutes: */N * * * *
	if strings.HasPrefix(minute, "*/") && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		n := strings.TrimPrefix(minute, "*/")
		if n == "1" {
			return "every minute"
		}
		return fmt.Sprintf("every %s minutes", n)
	}

	// Every N hours at :MM: MM */N * * *
	if strings.HasPrefix(hour, "*/") && dom == "*" && month == "*" && dow == "*" {
		n := strings.TrimPrefix(hour, "*/")
		if n == "1" {
			return fmt.Sprintf("every hour at :%s", padMinute(minute))
		}
		return fmt.Sprintf("every %s hours at :%s", n, padMinute(minute))
	}

	// Hourly within a range: MM H1-H2 * * *
	if strings.Contains(hour, "-") && !strings.Contains(hour, "/") && dom == "*" && month == "*" && dow == "*" && isSimpleCronField(minute) {
		parts := strings.SplitN(hour, "-", 2)
		return fmt.Sprintf("hourly %s:%s to %s:%s",
			padHour(parts[0]), padMinute(minute),
			padHour(parts[1]), padMinute(minute))
	}

	// Daily: MM HH * * *
	if dom == "*" && month == "*" && dow == "*" && simpleTime {
		return fmt.Sprintf("daily at %s", formatTime(hour, minute))
	}

	// Weekly: MM HH * * DOW (DOW may be a comma list like 1,3,5; describeDOW handles it)
	if dom == "*" && month == "*" && dow != "*" && simpleTime {
		return fmt.Sprintf("%s at %s", describeDOW(dow), formatTime(hour, minute))
	}

	// Monthly: MM HH DOM * *
	if month == "*" && dow == "*" && isSimpleCronField(dom) && simpleTime {
		return fmt.Sprintf("monthly on day %s at %s", dom, formatTime(hour, minute))
	}

	// Yearly: MM HH DOM MON *
	if dow == "*" && isSimpleCronField(month) && isSimpleCronField(dom) && simpleTime {
		return fmt.Sprintf("yearly on %s %s at %s", describeMonth(month), dom, formatTime(hour, minute))
	}

	return "custom schedule"
}

func padMinute(m string) string {
	if len(m) == 1 {
		return "0" + m
	}
	return m
}

func formatTime(hour, minute string) string {
	return fmt.Sprintf("%s:%s", padHour(hour), padMinute(minute))
}

func padHour(h string) string {
	if len(h) == 1 {
		return "0" + h
	}
	return h
}

var dowNames = map[string]string{
	"0": "Sunday", "7": "Sunday",
	"1": "Monday", "2": "Tuesday", "3": "Wednesday",
	"4": "Thursday", "5": "Friday", "6": "Saturday",
	"SUN": "Sunday", "MON": "Monday", "TUE": "Tuesday", "WED": "Wednesday",
	"THU": "Thursday", "FRI": "Friday", "SAT": "Saturday",
}

func describeDOW(dow string) string {
	// Handle comma-separated days.
	parts := strings.Split(dow, ",")
	if len(parts) > 1 {
		names := make([]string, 0, len(parts))
		for _, p := range parts {
			if n, ok := dowNames[strings.TrimSpace(strings.ToUpper(p))]; ok {
				names = append(names, n)
			} else {
				names = append(names, p)
			}
		}
		return strings.Join(names, ", ")
	}

	upper := strings.ToUpper(dow)
	if n, ok := dowNames[upper]; ok {
		return n
	}
	return "day " + dow
}

var monthNames = map[string]string{
	"1": "Jan", "2": "Feb", "3": "Mar", "4": "Apr",
	"5": "May", "6": "Jun", "7": "Jul", "8": "Aug",
	"9": "Sep", "10": "Oct", "11": "Nov", "12": "Dec",
	"JAN": "Jan", "FEB": "Feb", "MAR": "Mar", "APR": "Apr",
	"MAY": "May", "JUN": "Jun", "JUL": "Jul", "AUG": "Aug",
	"SEP": "Sep", "OCT": "Oct", "NOV": "Nov", "DEC": "Dec",
}

func describeMonth(m string) string {
	if n, ok := monthNames[strings.ToUpper(m)]; ok {
		return n
	}
	return m
}

// FormatNextRun returns a human-readable countdown like "in 2h 15m" or "in 5m".
func FormatNextRun(next time.Time) string {
	return formatDuration(time.Until(next))
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("in %dd %dh", days, hours)
		}
		return fmt.Sprintf("in %dd", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("in %dh %dm", hours, minutes)
		}
		return fmt.Sprintf("in %dh", hours)
	default:
		if minutes == 0 {
			return "in <1m"
		}
		return fmt.Sprintf("in %dm", minutes)
	}
}

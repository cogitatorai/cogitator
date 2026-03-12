package task

import (
	"testing"
	"time"
)



func TestDescribeCron(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"*/5 * * * *", "every 5 minutes"},
		{"*/1 * * * *", "every minute"},
		{"0 */2 * * *", "every 2 hours at :00"},
		{"30 */1 * * *", "every hour at :30"},
		{"0 9 * * *", "daily at 09:00"},
		{"30 14 * * *", "daily at 14:30"},
		{"0 8 * * 1", "Monday at 08:00"},
		{"0 9 * * 1,3,5", "Monday, Wednesday, Friday at 09:00"},
		{"0 9-21 * * *", "hourly 09:00 to 21:00"},
		{"0 0 1 * *", "monthly on day 1 at 00:00"},
		{"0 9 25 12 *", "yearly on Dec 25 at 09:00"},
		{"0 0 1 1 *", "yearly on Jan 1 at 00:00"},
		{"*/5 9-17 * * 1-5", "custom schedule"},
		{"manual", "manual"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got := DescribeCron(tt.expr)
			if got != tt.want {
				t.Errorf("DescribeCron(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"negative", -1 * time.Minute, "now"},
		{"zero", 0, "now"},
		{"30 seconds", 30 * time.Second, "in <1m"},
		{"5 minutes", 5 * time.Minute, "in 5m"},
		{"90 minutes", 90 * time.Minute, "in 1h 30m"},
		{"2 hours exactly", 2 * time.Hour, "in 2h"},
		{"27 hours", 27 * time.Hour, "in 1d 3h"},
		{"48 hours", 48 * time.Hour, "in 2d"},
		{"49 hours", 49 * time.Hour, "in 2d 1h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

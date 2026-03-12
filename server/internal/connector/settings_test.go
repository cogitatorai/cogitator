package connector

import (
	"path/filepath"
	"testing"
)

func TestSettings_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")

	s := NewSettingsStore(path)

	// Save calendar settings.
	cals := []CalendarEntry{
		{ID: "primary", Summary: "Andrei", Primary: true},
		{ID: "team@group.calendar.google.com", Summary: "Team", Primary: false},
	}
	if err := s.SetCalendars("google", "user1", cals); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabledCalendarIDs("google", "user1", []string{"primary", "team@group.calendar.google.com"}); err != nil {
		t.Fatal(err)
	}

	// Reload from disk.
	s2 := NewSettingsStore(path)
	got := s2.GetCalendars("google", "user1")
	if len(got) != 2 {
		t.Fatalf("calendars = %d, want 2", len(got))
	}
	if got[0].ID != "primary" {
		t.Fatalf("first calendar ID = %q", got[0].ID)
	}

	enabled := s2.GetEnabledCalendarIDs("google", "user1")
	if len(enabled) != 2 {
		t.Fatalf("enabled = %d, want 2", len(enabled))
	}
}

func TestSettings_DefaultsToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")

	s := NewSettingsStore(path)
	cals := s.GetCalendars("google", "user1")
	if len(cals) != 0 {
		t.Fatalf("expected empty calendars, got %d", len(cals))
	}
	enabled := s.GetEnabledCalendarIDs("google", "user1")
	if len(enabled) != 0 {
		t.Fatalf("expected empty enabled, got %d", len(enabled))
	}
}

func TestSettings_DeleteUserSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")

	s := NewSettingsStore(path)
	s.SetCalendars("google", "user1", []CalendarEntry{{ID: "primary", Summary: "Me", Primary: true}})
	s.SetEnabledCalendarIDs("google", "user1", []string{"primary"})

	if err := s.DeleteUserSettings("google", "user1"); err != nil {
		t.Fatal(err)
	}

	if cals := s.GetCalendars("google", "user1"); len(cals) != 0 {
		t.Fatal("expected empty after delete")
	}
}

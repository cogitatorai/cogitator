package connector

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// CalendarEntry represents a calendar discovered from the provider.
type CalendarEntry struct {
	ID      string `yaml:"id" json:"id"`
	Summary string `yaml:"summary" json:"summary"`
	Primary bool   `yaml:"primary" json:"primary"`
}

// UserConnectorSettings holds per-user settings for a single connector.
type UserConnectorSettings struct {
	Calendars          []CalendarEntry `yaml:"calendars,omitempty" json:"calendars"`
	EnabledCalendarIDs []string        `yaml:"enabled_calendar_ids,omitempty" json:"enabled_calendar_ids"`
}

// settingsFile is the top-level YAML structure.
type settingsFile struct {
	Connectors map[string]map[string]*UserConnectorSettings `yaml:"connectors"`
}

// SettingsStore manages connector settings persisted to a YAML file.
type SettingsStore struct {
	mu   sync.RWMutex
	path string
	data settingsFile
}

// NewSettingsStore creates a store, loading existing data from path if present.
func NewSettingsStore(path string) *SettingsStore {
	s := &SettingsStore{
		path: path,
		data: settingsFile{Connectors: make(map[string]map[string]*UserConnectorSettings)},
	}
	_ = s.load()
	return s
}

func (s *SettingsStore) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var f settingsFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return err
	}
	if f.Connectors != nil {
		s.data = f
	}
	return nil
}

func (s *SettingsStore) save() error {
	raw, err := yaml.Marshal(s.data)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

func (s *SettingsStore) userSettings(connector, userID string) *UserConnectorSettings {
	users := s.data.Connectors[connector]
	if users == nil {
		return nil
	}
	return users[userID]
}

func (s *SettingsStore) ensureUserSettings(connector, userID string) *UserConnectorSettings {
	if s.data.Connectors[connector] == nil {
		s.data.Connectors[connector] = make(map[string]*UserConnectorSettings)
	}
	if s.data.Connectors[connector][userID] == nil {
		s.data.Connectors[connector][userID] = &UserConnectorSettings{}
	}
	return s.data.Connectors[connector][userID]
}

// GetCalendars returns cached calendar entries for a connector+user.
func (s *SettingsStore) GetCalendars(connector, userID string) []CalendarEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	us := s.userSettings(connector, userID)
	if us == nil {
		return nil
	}
	return us.Calendars
}

// SetCalendars updates the cached calendar list and persists to disk.
func (s *SettingsStore) SetCalendars(connector, userID string, calendars []CalendarEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	us := s.ensureUserSettings(connector, userID)
	us.Calendars = calendars
	return s.save()
}

// GetEnabledCalendarIDs returns the user's selected calendar IDs.
func (s *SettingsStore) GetEnabledCalendarIDs(connector, userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	us := s.userSettings(connector, userID)
	if us == nil {
		return nil
	}
	return us.EnabledCalendarIDs
}

// SetEnabledCalendarIDs saves the user's calendar selection.
func (s *SettingsStore) SetEnabledCalendarIDs(connector, userID string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	us := s.ensureUserSettings(connector, userID)
	us.EnabledCalendarIDs = ids
	return s.save()
}

// DeleteUserSettings removes all settings for a connector+user pair.
func (s *SettingsStore) DeleteUserSettings(connector, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if users := s.data.Connectors[connector]; users != nil {
		delete(users, userID)
	}
	return s.save()
}

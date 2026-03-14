package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the persisted browser connector settings.
type Config struct {
	Enabled    bool   `yaml:"enabled"`
	Port       int    `yaml:"port"`
	Managed    bool   `yaml:"managed"`
	ChromePath string `yaml:"chrome_path,omitempty"`
}

// ConnectorStatus is the current state returned by the Status() method.
type ConnectorStatus struct {
	Enabled       bool   `json:"enabled"`
	Connected     bool   `json:"connected"`
	Managed       bool   `json:"managed"`
	Port          int    `json:"port"`
	ChromeVersion string `json:"chrome_version,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Connector manages the Chrome browser connection lifecycle.
type Connector struct {
	mu             sync.Mutex
	config         Config
	configPath     string
	client         *Client
	process        *exec.Cmd // only set in managed mode
	connected      bool
	chromeVersion  string
	lastError      string
	logger         *slog.Logger
	pollTicker     *time.Ticker
	pollDone       chan struct{}
	onToolsChanged func()

	// pollInterval can be overridden in tests to avoid 30s waits.
	pollInterval time.Duration

	// wsURLOverride bypasses DiscoverWSURL when set (used by tests).
	wsURLOverride string

	sessions *sessionCache
}

// NewConnector creates a Connector rooted at workspaceDir.
// It loads any existing config from browser_connector.yaml and defaults the
// port to 9222 when no config file is present.
func NewConnector(workspaceDir string, logger *slog.Logger) *Connector {
	c := &Connector{
		configPath:   filepath.Join(workspaceDir, "browser_connector.yaml"),
		logger:       logger,
		pollInterval: 30 * time.Second,
		sessions:     newSessionCache(),
	}
	c.loadConfig()
	if c.config.Port == 0 {
		c.config.Port = 9222
	}
	return c
}

// Enable activates the connector. In managed mode it starts a headless Chrome
// process. It attempts an initial connection but returns nil even when Chrome
// is unreachable; the background poller will reconnect automatically.
func (c *Connector) Enable() error {
	c.mu.Lock()
	c.config.Enabled = true

	if c.config.Managed {
		chromePath := DetectChromePath(c.config.ChromePath)
		if chromePath == "" {
			c.mu.Unlock()
			return fmt.Errorf("chrome binary not found")
		}
		cmd, err := StartHeadless(chromePath, c.config.Port)
		if err != nil {
			c.mu.Unlock()
			return fmt.Errorf("starting headless chrome: %w", err)
		}
		c.process = cmd

		// Monitor for early exit so we can surface the error.
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		c.mu.Unlock()

		// Poll until Chrome's debug port is reachable (up to 10s).
		deadline := time.Now().Add(10 * time.Second)
		started := false
		for time.Now().Before(deadline) {
			select {
			case err := <-exited:
				// Chrome died before binding the port. Most common cause:
				// another Chrome instance holds the profile lock.
				c.mu.Lock()
				c.process = nil
				c.lastError = "chrome exited immediately; close Chrome and retry"
				c.mu.Unlock()
				return fmt.Errorf("chrome exited before binding debug port (is Chrome already running?): %v", err)
			default:
			}
			if _, err := GetVersion(fmt.Sprintf("http://127.0.0.1:%d", c.config.Port)); err == nil {
				started = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !started {
			// Chrome is running but port isn't bound yet; proceed anyway,
			// the polling loop will pick it up.
			c.logger.Warn("browser connector: chrome started but debug port not yet reachable")
		}
		c.mu.Lock()
	}

	// Best-effort initial connection; errors are swallowed intentionally.
	if err := c.connectLocked(); err != nil {
		c.lastError = err.Error()
		c.logger.Info("browser connector: initial connect failed, will retry",
			"error", err)
	}

	c.startPollingLocked()
	c.mu.Unlock()

	if cb := c.onToolsChanged; cb != nil {
		cb()
	}

	return c.saveConfig()
}

// Disable deactivates the connector, stops polling, closes any open client,
// and terminates a managed Chrome process.
func (c *Connector) Disable() error {
	c.mu.Lock()
	c.config.Enabled = false
	c.stopPollingLocked()

	client := c.client
	c.client = nil
	c.connected = false
	c.chromeVersion = ""
	c.sessions.clear()

	proc := c.process
	c.process = nil
	c.mu.Unlock()

	if client != nil {
		client.Close()
	}
	StopProcess(proc)

	if cb := c.onToolsChanged; cb != nil {
		cb()
	}

	return c.saveConfig()
}

// IsEnabled reports whether the connector is enabled.
func (c *Connector) IsEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.Enabled
}

// IsConnected reports whether a live CDP connection exists.
func (c *Connector) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Status returns a snapshot of the current connector state.
func (c *Connector) Status() ConnectorStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ConnectorStatus{
		Enabled:       c.config.Enabled,
		Connected:     c.connected,
		Managed:       c.config.Managed,
		Port:          c.config.Port,
		ChromeVersion: c.chromeVersion,
		Error:         c.lastError,
	}
}

// OnToolsChanged registers a callback that is invoked whenever the set of
// available tools changes (on Enable or Disable).
func (c *Connector) OnToolsChanged(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onToolsChanged = fn
}

// UpdateConfig applies new connection settings. If the connector is currently
// enabled it reconnects with the updated configuration.
func (c *Connector) UpdateConfig(port int, managed bool, chromePath string) error {
	c.mu.Lock()
	c.config.Port = port
	c.config.Managed = managed
	c.config.ChromePath = chromePath
	enabled := c.config.Enabled
	c.mu.Unlock()

	if err := c.saveConfig(); err != nil {
		return err
	}
	if enabled {
		if err := c.Disable(); err != nil {
			c.logger.Warn("browser connector: disable before reconnect failed", "error", err)
		}
		return c.Enable()
	}
	return nil
}

// SaveConfig persists the current config to disk. It is exported so that
// callers can snapshot config changes made directly to the Config field during
// tests.
func (c *Connector) SaveConfig() error {
	return c.saveConfig()
}

// Client returns the underlying CDP client. Returns nil when not connected.
func (c *Connector) Client() *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

// connectLocked attempts to establish a CDP connection. Must be called with
// c.mu held.
func (c *Connector) connectLocked() error {
	var wsURL string
	var err error
	if c.wsURLOverride != "" {
		wsURL = c.wsURLOverride
	} else {
		wsURL, err = DiscoverWSURL(c.config.Port)
		if err != nil {
			return err
		}
	}

	if wsURL == "" {
		return fmt.Errorf("chrome returned empty webSocketDebuggerUrl")
	}

	client := NewClient()
	client.OnClose(func() {
		c.mu.Lock()
		c.connected = false
		c.logger.Info("browser connector: CDP connection closed")
		c.mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		return fmt.Errorf("CDP connect: %w", err)
	}

	// Close any prior client before replacing it.
	if c.client != nil {
		go c.client.Close()
	}

	c.client = client
	c.connected = true
	c.lastError = ""
	c.sessions.clear()

	// Fetch Chrome version via CDP (HTTP endpoints may not be available).
	if ver, err := GetVersionCDP(ctx, client); err == nil {
		c.chromeVersion = ver
	}
	return nil
}

// startPollingLocked starts the background reconnect goroutine. Must be called
// with c.mu held. If a poller is already running it is stopped first.
func (c *Connector) startPollingLocked() {
	c.stopPollingLocked()
	ticker := time.NewTicker(c.pollInterval)
	done := make(chan struct{})
	c.pollTicker = ticker
	c.pollDone = done
	go c.poll(ticker, done)
}

// stopPollingLocked stops the background poller if one is running. Must be
// called with c.mu held.
func (c *Connector) stopPollingLocked() {
	if c.pollTicker != nil {
		c.pollTicker.Stop()
		c.pollTicker = nil
	}
	if c.pollDone != nil {
		close(c.pollDone)
		c.pollDone = nil
	}
}

// poll runs the reconnect loop. It is launched as a goroutine by
// startPollingLocked and exits when done is closed.
func (c *Connector) poll(ticker *time.Ticker, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			c.mu.Lock()
			if !c.connected {
				err := c.connectLocked()
				if err == nil {
					c.mu.Unlock()
					if cb := c.onToolsChanged; cb != nil {
						cb()
					}
				} else {
					c.lastError = err.Error()
					c.mu.Unlock()
				}
			} else {
				c.mu.Unlock()
			}
		}
	}
}

// loadConfig reads the YAML config file. If the file does not exist or cannot
// be parsed, the config is left at its zero value.
func (c *Connector) loadConfig() {
	data, err := os.ReadFile(c.configPath)
	if err != nil {
		return // file not found is expected on first run
	}
	if err := yaml.Unmarshal(data, &c.config); err != nil {
		c.logger.Warn("browser connector: failed to parse config", "error", err)
	}
}

// saveConfig marshals the current config to the YAML file.
func (c *Connector) saveConfig() error {
	c.mu.Lock()
	cfg := c.config
	path := c.configPath
	c.mu.Unlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling browser config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing browser config: %w", err)
	}
	return nil
}

package fly

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultBaseURL = "https://api.machines.dev/v1"

// MachineAPI is the interface consumed by the provisioner, allowing test
// doubles to replace the real Fly Machines client.
type MachineAPI interface {
	CreateVolume(name string, sizeGB int, region string) (*Volume, error)
	DeleteVolume(volumeID string) error
	CreateMachine(cfg MachineConfig, region string) (*Machine, error)
	UpdateMachine(machineID string, image string) error
	StartMachine(machineID string) error
	StopMachine(machineID string) error
	DestroyMachine(machineID string) error
	GetMachine(machineID string) (*Machine, error)
}

// MachineConfig describes the desired configuration for a new machine.
type MachineConfig struct {
	Name     string
	Image    string
	CPUs     int
	MemoryMB int
	Env      map[string]string
	Secrets  map[string]string
	VolumeID string
}

// Machine represents a Fly Machine as returned by the API.
type Machine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Region string `json:"region"`
}

// Volume represents a Fly Volume as returned by the API.
type Volume struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SizeGB int    `json:"size_gb"`
	State  string `json:"state"`
}

// Client is a thin wrapper around the Fly Machines REST API.
type Client struct {
	token   string
	appName string
	baseURL string
	client  *http.Client
}

// compile-time check
var _ MachineAPI = (*Client)(nil)

// NewClient returns a Client configured for the given app.
func NewClient(token, appName string) *Client {
	return &Client{
		token:   token,
		appName: appName,
		baseURL: defaultBaseURL,
		client:  http.DefaultClient,
	}
}

// CreateVolume provisions a persistent volume in the specified region.
func (c *Client) CreateVolume(name string, sizeGB int, region string) (*Volume, error) {
	body := map[string]any{
		"name":   name,
		"size_gb": sizeGB,
		"region": region,
	}
	var vol Volume
	if err := c.do(http.MethodPost, fmt.Sprintf("/apps/%s/volumes", c.appName), body, &vol); err != nil {
		return nil, err
	}
	return &vol, nil
}

// DeleteVolume removes a volume by ID.
func (c *Client) DeleteVolume(volumeID string) error {
	return c.do(http.MethodDelete, fmt.Sprintf("/apps/%s/volumes/%s", c.appName, volumeID), nil, nil)
}

// CreateMachine launches a new machine with the given config and region.
// It wires up port 8484 as an internal TCP service and attaches the volume
// (if specified) at /data.
func (c *Client) CreateMachine(cfg MachineConfig, region string) (*Machine, error) {
	services := []map[string]any{
		{
			"ports": []map[string]any{
				{"port": 8484, "handlers": []string{"http"}},
			},
			"protocol":      "tcp",
			"internal_port": 8484,
		},
	}

	machineConfig := map[string]any{
		"image": cfg.Image,
		"env":   cfg.Env,
		"guest": map[string]any{
			"cpus":      cfg.CPUs,
			"memory_mb": cfg.MemoryMB,
		},
		"services": services,
	}

	if len(cfg.Secrets) > 0 {
		secretsList := make([]map[string]string, 0, len(cfg.Secrets))
		for k, v := range cfg.Secrets {
			secretsList = append(secretsList, map[string]string{"env_var": k, "value": v})
		}
		machineConfig["secrets"] = secretsList
	}

	if cfg.VolumeID != "" {
		machineConfig["mounts"] = []map[string]any{
			{"volume": cfg.VolumeID, "path": "/data"},
		}
	}

	body := map[string]any{
		"name":   cfg.Name,
		"region": region,
		"config": machineConfig,
	}

	var m Machine
	if err := c.do(http.MethodPost, fmt.Sprintf("/apps/%s/machines", c.appName), body, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateMachine replaces the machine image (rolling update).
func (c *Client) UpdateMachine(machineID string, image string) error {
	body := map[string]any{
		"config": map[string]any{
			"image": image,
		},
	}
	return c.do(http.MethodPost, fmt.Sprintf("/apps/%s/machines/%s", c.appName, machineID), body, nil)
}

// StartMachine sends a start signal to a stopped machine.
func (c *Client) StartMachine(machineID string) error {
	return c.do(http.MethodPost, fmt.Sprintf("/apps/%s/machines/%s/start", c.appName, machineID), nil, nil)
}

// StopMachine sends a stop signal to a running machine.
func (c *Client) StopMachine(machineID string) error {
	return c.do(http.MethodPost, fmt.Sprintf("/apps/%s/machines/%s/stop", c.appName, machineID), nil, nil)
}

// DestroyMachine permanently removes a machine.
func (c *Client) DestroyMachine(machineID string) error {
	return c.do(http.MethodDelete, fmt.Sprintf("/apps/%s/machines/%s", c.appName, machineID), nil, nil)
}

// GetMachine retrieves the current state of a machine.
func (c *Client) GetMachine(machineID string) (*Machine, error) {
	var m Machine
	if err := c.do(http.MethodGet, fmt.Sprintf("/apps/%s/machines/%s", c.appName, machineID), nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// do executes an authenticated request against the Fly Machines API.
// It marshals body as JSON (if non-nil), sets the auth header, and
// unmarshals the response into out (if non-nil). Non-2xx responses
// are returned as errors containing the response body.
func (c *Client) do(method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("fly: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("fly: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("fly: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fly: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("fly: %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("fly: unmarshal response: %w", err)
		}
	}
	return nil
}

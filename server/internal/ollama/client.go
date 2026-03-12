package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const DefaultBaseURL = "http://localhost:11434"

// Client talks to the Ollama REST API for model management.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates an Ollama client. Pass "" for baseURL to use the default.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Status checks whether Ollama is running by pinging the root endpoint.
func (c *Client) Status(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.baseURL+"/", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ListModels returns all locally available models.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: status %d", resp.StatusCode)
	}

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	models := make([]Model, len(tags.Models))
	for i, t := range tags.Models {
		models[i] = Model{
			Name:       t.Name,
			Size:       t.Size,
			Family:     t.Details.Family,
			Parameters: t.Details.ParameterSize,
			Quant:      t.Details.QuantizationLevel,
			ModifiedAt: t.ModifiedAt,
		}
	}
	return models, nil
}

// PullModel downloads a model, sending progress events to the channel.
// The channel is closed when the pull completes or fails.
// Uses a separate HTTP client with no timeout (pulls can take minutes).
func (c *Client) PullModel(ctx context.Context, name string, progress chan<- PullProgress) error {
	defer close(progress)

	body, _ := json.Marshal(map[string]any{"name": name, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// No timeout: pulls can take minutes for large models.
	pullClient := &http.Client{}
	resp, err := pullClient.Do(req)
	if err != nil {
		return fmt.Errorf("pull model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull model: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var p PullProgress
		if err := json.Unmarshal(line, &p); err != nil {
			continue
		}
		if p.Error != "" {
			progress <- p
			return fmt.Errorf("ollama: %s", p.Error)
		}
		progress <- p
		if p.Status == "success" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}

// DeleteModel removes a locally pulled model.
func (c *Client) DeleteModel(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete model: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete model: status %d", resp.StatusCode)
	}
	return nil
}

package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

// DNSAPI defines the operations for managing DNS records.
type DNSAPI interface {
	AddCNAME(subdomain, target string, proxied bool) (*DNSRecord, error)
	DeleteRecord(recordID string) error
	FindRecord(name string) (*DNSRecord, error)
}

// DNSRecord represents a Cloudflare DNS record.
type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// Client communicates with the Cloudflare DNS API.
type Client struct {
	token   string
	zoneID  string
	baseURL string
	client  *http.Client
}

// NewClient returns a Client configured with the given API token and zone ID.
func NewClient(token, zoneID string) *Client {
	return &Client{
		token:   token,
		zoneID:  zoneID,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}
}

// AddCNAME creates a CNAME record pointing subdomain to target.
func (c *Client) AddCNAME(subdomain, target string, proxied bool) (*DNSRecord, error) {
	body := map[string]any{
		"type":    "CNAME",
		"name":    subdomain,
		"content": target,
		"proxied": proxied,
	}
	var resp apiResponse[DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records", c.zoneID)
	if err := c.do(http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

// DeleteRecord removes a DNS record by ID.
func (c *Client) DeleteRecord(recordID string) error {
	path := fmt.Sprintf("/zones/%s/dns_records/%s", c.zoneID, recordID)
	return c.do(http.MethodDelete, path, nil, nil)
}

// FindRecord looks up a DNS record by exact name. Returns nil with no error
// when no matching record exists.
func (c *Client) FindRecord(name string) (*DNSRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?name=%s", c.zoneID, name)
	var resp apiResponse[[]DNSRecord]
	if err := c.do(http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Result) == 0 {
		return nil, nil
	}
	return &resp.Result[0], nil
}

// apiResponse is the envelope Cloudflare wraps around every response.
type apiResponse[T any] struct {
	Success bool     `json:"success"`
	Errors  []apiErr `json:"errors"`
	Result  T        `json:"result"`
}

type apiErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// do executes an HTTP request against the Cloudflare API.
func (c *Client) do(method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cloudflare: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("cloudflare: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cloudflare: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("cloudflare: %s %s returned %d: %s", method, path, resp.StatusCode, raw)
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("cloudflare: decode response: %w", err)
		}
	}
	return nil
}

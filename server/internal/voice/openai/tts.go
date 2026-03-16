package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type TTS struct {
	client *Client
}

func NewTTS(client *Client) *TTS {
	return &TTS{client: client}
}

type ttsRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

func (s *TTS) Synthesize(text string, voiceName string) (io.ReadCloser, error) {
	body, err := json.Marshal(ttsRequest{
		Model:          "tts-1",
		Input:          text,
		Voice:          voiceName,
		ResponseFormat: "aac",
	})
	if err != nil {
		return nil, fmt.Errorf("voice/openai: marshal request: %w", err)
	}

	url := strings.TrimRight(s.client.BaseURL, "/") + "/v1/audio/speech"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("voice/openai: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.client.APIKey)

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice/openai: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("voice/openai: API returned %d: %s", resp.StatusCode, errBody)
	}

	return resp.Body, nil
}

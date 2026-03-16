package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/voice"
)

type STT struct {
	client *Client
}

func NewSTT(client *Client) *STT {
	return &STT{client: client}
}

func (s *STT) Transcribe(ctx context.Context, audio []byte, format string) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "audio."+format)
	if err != nil {
		return "", fmt.Errorf("voice/openai: create form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(audio)); err != nil {
		return "", fmt.Errorf("voice/openai: copy audio: %w", err)
	}
	writer.WriteField("model", "whisper-1")
	writer.Close()

	url := strings.TrimRight(s.client.BaseURL, "/") + "/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", fmt.Errorf("voice/openai: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.client.APIKey)

	resp, err := s.client.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("voice/openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("voice/openai: API returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("voice/openai: decode response: %w", err)
	}

	if result.Text == "" {
		return "", voice.ErrTranscriptionEmpty
	}
	return result.Text, nil
}

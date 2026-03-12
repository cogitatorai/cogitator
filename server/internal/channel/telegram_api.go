package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	telegramBaseURL      = "https://api.telegram.org/bot"
	telegramMaxMsgLength = 4096
)

// telegramResponse is the generic envelope returned by every Telegram Bot API call.
type telegramResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description,omitempty"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramMessage struct {
	MessageID int         `json:"message_id"`
	Chat      telegramChat `json:"chat"`
	Text      string      `json:"text,omitempty"`
}

type telegramUpdate struct {
	UpdateID int             `json:"update_id"`
	Message  telegramMessage `json:"message"`
}

type telegramClient struct {
	token      string
	httpClient *http.Client
}

func newTelegramClient(token string) *telegramClient {
	return &telegramClient{
		token: token,
		httpClient: &http.Client{
			Timeout: 0, // no overall timeout; long-poll requests set their own deadline
		},
	}
}

func (c *telegramClient) url(method string) string {
	return telegramBaseURL + c.token + "/" + method
}

// getUpdates fetches pending updates via long polling.
// offset is the next expected update_id. timeout is the long-poll duration in seconds.
func (c *telegramClient) getUpdates(ctx context.Context, offset, timeout int) ([]telegramUpdate, error) {
	body, err := json.Marshal(map[string]any{
		"offset":  offset,
		"timeout": timeout,
		"allowed_updates": []string{"message"},
	})
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("getUpdates"), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates do: %w", err)
	}
	defer resp.Body.Close()

	var result telegramResponse[[]telegramUpdate]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("telegram getUpdates decode: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram getUpdates api error: %s", result.Description)
	}
	return result.Result, nil
}

// sendMessage sends a text message to the given chatID.
// Messages exceeding the 4096-character Telegram limit are split automatically.
func (c *telegramClient) sendMessage(ctx context.Context, chatID int64, text string) error {
	for len(text) > 0 {
		chunk := text
		if len(chunk) > telegramMaxMsgLength {
			// Split at the last newline within the allowed window to preserve
			// readability; fall back to a hard cut if none exists.
			cutAt := telegramMaxMsgLength
			for i := telegramMaxMsgLength - 1; i > telegramMaxMsgLength/2; i-- {
				if text[i] == '\n' {
					cutAt = i + 1
					break
				}
			}
			chunk = text[:cutAt]
			text = text[cutAt:]
		} else {
			text = ""
		}

		if err := c.sendSingleMessage(ctx, chatID, chunk); err != nil {
			return err
		}

		// Respect Telegram's rate limits between chunks.
		if len(text) > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}

func (c *telegramClient) sendSingleMessage(ctx context.Context, chatID int64, text string) error {
	// Try HTML with Markdown-to-HTML conversion first; fall back to plain
	// text if Telegram still rejects it.
	html := markdownToTelegramHTML(text)
	if err := c.postMessage(ctx, chatID, html, "HTML"); err == nil {
		return nil
	}
	return c.postMessage(ctx, chatID, text, "")
}

func (c *telegramClient) postMessage(ctx context.Context, chatID int64, text, parseMode string) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram sendMessage marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram sendMessage new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram sendMessage do: %w", err)
	}
	defer resp.Body.Close()

	var result telegramResponse[telegramMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("telegram sendMessage decode: %w", err)
	}
	if !result.OK {
		if parseMode != "" {
			return fmt.Errorf("telegram sendMessage api error: %s", result.Description)
		}
		return fmt.Errorf("telegram sendMessage api error: %s", result.Description)
	}
	return nil
}

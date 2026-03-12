package push

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"strings"
)

const expoAPIURL = "https://exp.host/--/api/v2/push/send"

// Message is the Expo push notification payload.
type Message struct {
	To    string         `json:"to"`
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Badge *int           `json:"badge,omitempty"`
	Sound string         `json:"sound,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// expoResponse is the Expo Push API response envelope.
type expoResponse struct {
	Data []expoTicket `json:"data"`
}

type expoTicket struct {
	Status  string       `json:"status"` // "ok" or "error"
	ID      string       `json:"id,omitempty"`
	Details *expoDetails `json:"details,omitempty"`
}

type expoDetails struct {
	Error string `json:"error,omitempty"` // e.g. "DeviceNotRegistered"
}

// Sender sends push notifications via the Expo Push API.
type Sender struct {
	store  *Store
	client *http.Client
	logger *slog.Logger
	apiURL string
}

func NewSender(store *Store, logger *slog.Logger) *Sender {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sender{
		store:  store,
		client: &http.Client{},
		logger: logger,
		apiURL: expoAPIURL,
	}
}

// Send pushes a notification to all tokens for the given user.
// Returns the number of tokens sent to.
func (s *Sender) Send(userID string, title, body string, badge int, data map[string]any) int {
	tokens, err := s.store.ListByUser(userID)
	if err != nil {
		s.logger.Error("push: list tokens", "user", userID, "error", err)
		return 0
	}
	if len(tokens) == 0 {
		return 0
	}
	s.logger.Info("push: sending", "user", userID, "tokens", len(tokens))

	var messages []Message
	for _, tok := range tokens {
		msg := Message{
			To:    tok.Token,
			Title: title,
			Body:  body,
			Sound: "default",
			Data:  data,
		}
		if strings.ToLower(tok.Platform) == "ios" {
			msg.Badge = &badge
		}
		messages = append(messages, msg)
	}

	payload, err := json.Marshal(messages)
	if err != nil {
		s.logger.Error("push: marshal", "error", err)
		return 0
	}

	req, err := http.NewRequest("POST", s.apiURL, bytes.NewReader(payload))
	if err != nil {
		s.logger.Error("push: create request", "error", err)
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("push: send", "error", err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("push: non-200 response", "status", resp.StatusCode)
		return 0
	}

	var expoResp expoResponse
	if err := json.NewDecoder(resp.Body).Decode(&expoResp); err != nil {
		s.logger.Error("push: decode response", "error", err)
		return len(tokens)
	}

	// Clean up invalid tokens.
	for i, ticket := range expoResp.Data {
		if ticket.Status == "error" && ticket.Details != nil &&
			ticket.Details.Error == "DeviceNotRegistered" {
			if i < len(tokens) {
				s.logger.Info("push: removing invalid token", "token", tokens[i].Token)
				s.store.DeleteByToken(tokens[i].Token)
			}
		}
	}

	return len(tokens)
}

// SendToUser is a convenience wrapper that builds the data map from type and optional fields.
func (s *Sender) SendToUser(userID, title, body, notifType string, badge int, extra map[string]any) int {
	data := map[string]any{"type": notifType}
	maps.Copy(data, extra)
	return s.Send(userID, title, body, badge, data)
}

// SendToAll sends a push notification to all registered tokens across all users.
func (s *Sender) SendToAll(title, body, notifType string, extra map[string]any) int {
	tokens, err := s.store.ListAll()
	if err != nil {
		s.logger.Error("push: list all tokens", "error", err)
		return 0
	}
	if len(tokens) == 0 {
		s.logger.Info("push: no tokens registered (broadcast)")
		return 0
	}

	data := map[string]any{"type": notifType}
	maps.Copy(data, extra)

	var messages []Message
	for _, tok := range tokens {
		msg := Message{
			To:    tok.Token,
			Title: title,
			Body:  body,
			Sound: "default",
			Data:  data,
		}
		messages = append(messages, msg)
	}

	payload, err := json.Marshal(messages)
	if err != nil {
		s.logger.Error("push: marshal", "error", err)
		return 0
	}

	req, err := http.NewRequest("POST", s.apiURL, bytes.NewReader(payload))
	if err != nil {
		s.logger.Error("push: create request", "error", err)
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("push: send broadcast", "error", err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("push: non-200 response (broadcast)", "status", resp.StatusCode)
		return 0
	}

	s.logger.Info("push: broadcast sent", "tokens", len(tokens), "title", title)
	return len(tokens)
}

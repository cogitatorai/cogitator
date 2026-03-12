package channel

import "context"

// IncomingMessage represents a message received from any channel.
type IncomingMessage struct {
	Channel    string
	ChatID     string
	SessionKey string
	UserID     string
	Text       string
	Private    bool
}

// OutgoingMessage represents a message sent back through a channel.
type OutgoingMessage struct {
	ChatID  string
	Text    string
	IsError bool
}

// Channel defines the interface all interaction channels must implement.
// Each channel (web, telegram, whatsapp) receives messages from its transport,
// converts them to IncomingMessage, and sends OutgoingMessage back.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
}

// HandlerResponse carries the result of processing a chat message.
type HandlerResponse struct {
	Content   string // The assistant's reply text.
	ToolsUsed string // JSON-encoded resolved tools, empty when no tools were used.
}

// MessageHandler is the callback a channel invokes when a message arrives.
type MessageHandler func(ctx context.Context, msg IncomingMessage) (HandlerResponse, error)

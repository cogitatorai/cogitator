package channel

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/session"
)

const (
	telegramLongPollTimeout = 30  // seconds
	telegramRetryDelay      = 5 * time.Second
	telegramStopTimeout     = 10 * time.Second
)

// TelegramChannel implements Channel for the Telegram Bot API.
// It uses long-polling to receive messages and sends replies via sendMessage.
type TelegramChannel struct {
	handler     MessageHandler
	eventBus    *bus.Bus
	sessions    *session.Store
	configStore *config.Store
	logger      *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewTelegramChannel constructs a TelegramChannel with the given dependencies.
func NewTelegramChannel(
	handler MessageHandler,
	eventBus *bus.Bus,
	sessions *session.Store,
	configStore *config.Store,
	logger *slog.Logger,
) *TelegramChannel {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelegramChannel{
		handler:     handler,
		eventBus:    eventBus,
		sessions:    sessions,
		configStore: configStore,
		logger:      logger,
	}
}

func (t *TelegramChannel) Name() string { return "telegram" }

// Start reads config and, if the channel is enabled with a token set, begins polling.
// Calling Start on an already-running channel stops it first.
func (t *TelegramChannel) Start(ctx context.Context) error {
	t.mu.Lock()
	running := t.cancel != nil
	t.mu.Unlock()
	if running {
		if err := t.Stop(); err != nil {
			return err
		}
	}

	cfg := t.configStore.Get()
	tgCfg := cfg.Channels.Telegram

	if !tgCfg.Enabled || tgCfg.BotToken == "" {
		t.logger.Info("telegram channel disabled or no token configured")
		return nil
	}

	t.startPolling(ctx, tgCfg)
	return nil
}

// Stop cancels the polling context and waits for all goroutines to exit.
func (t *TelegramChannel) Stop() error {
	t.mu.Lock()
	cancel := t.cancel
	t.cancel = nil
	t.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	// Wait for both pollLoop and notificationLoop with a timeout.
	waitDone := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(telegramStopTimeout):
		t.logger.Warn("telegram channel stop timed out")
	}
	return nil
}

// Restart stops any running poll loop and starts a fresh one. Call this after
// a config change to apply the new token or allow-list without restarting the
// entire application.
func (t *TelegramChannel) Restart(ctx context.Context) error {
	if err := t.Stop(); err != nil {
		return err
	}
	return t.Start(ctx)
}

// startPolling creates a cancellable context, builds the client and allow-list,
// and launches the poll and notification goroutines.
func (t *TelegramChannel) startPolling(parentCtx context.Context, tgCfg config.TelegramChannelConfig) {
	ctx, cancel := context.WithCancel(parentCtx)

	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()

	client := newTelegramClient(tgCfg.BotToken)

	allowed := make(map[int64]bool, len(tgCfg.AllowedChatIDs))
	for _, id := range tgCfg.AllowedChatIDs {
		allowed[id] = true
	}

	t.wg.Add(2)
	go func() {
		defer t.wg.Done()
		t.pollLoop(ctx, client, allowed)
	}()
	go func() {
		defer t.wg.Done()
		t.notificationLoop(ctx, client, allowed)
	}()
}

// pollLoop calls getUpdates in a tight loop, dispatches each incoming text
// message to the handler, and sends the response back to Telegram.
// It closes done when it exits so Stop can unblock.
func (t *TelegramChannel) pollLoop(
	ctx context.Context,
	client *telegramClient,
	allowed map[int64]bool,
) {

	offset := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := client.getUpdates(ctx, offset, telegramLongPollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.logger.Error("telegram getUpdates failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(telegramRetryDelay):
			}
			continue
		}

		for _, upd := range updates {
			offset = upd.UpdateID + 1

			text := strings.TrimSpace(upd.Message.Text)
			if text == "" {
				continue
			}

			chatID := upd.Message.Chat.ID

			if len(allowed) > 0 && !allowed[chatID] {
				t.logger.Warn("telegram message from disallowed chat", "chat_id", chatID)
				continue
			}

			chatIDStr := strconv.FormatInt(chatID, 10)
			sessionKey := "telegram:" + chatIDStr

			if t.sessions != nil {
				if _, err := t.sessions.GetOrCreate(sessionKey, "telegram", chatIDStr, "", false); err != nil {
					t.logger.Error("telegram session get/create failed", "error", err, "session_key", sessionKey)
				}
				if err := t.sessions.SetActiveSession(sessionKey, ""); err != nil {
					t.logger.Warn("telegram set active session failed", "error", err, "session_key", sessionKey)
				}
			}

			// Handle bot commands locally without touching the agent.
			if text == "/start" {
				client.sendMessage(ctx, chatID, "Ready.")
				continue
			}
			if text == "/clear" {
				if t.sessions != nil {
					if err := t.sessions.TruncateMessages(sessionKey, 0); err != nil {
						t.logger.Error("telegram clear failed", "error", err, "session_key", sessionKey)
						client.sendMessage(ctx, chatID, "Failed to clear history.")
					} else {
						client.sendMessage(ctx, chatID, "Conversation history cleared.")
					}
				}
				continue
			}

			incoming := IncomingMessage{
				Channel:    "telegram",
				ChatID:     chatIDStr,
				SessionKey: sessionKey,
				Text:       text,
			}

			resp, handlerErr := t.handler(ctx, incoming)
			if handlerErr != nil {
				t.logger.Error("telegram handler error", "error", handlerErr, "chat_id", chatID)
				if sendErr := client.sendMessage(ctx, chatID, "Sorry, I encountered an error processing your message."); sendErr != nil {
					t.logger.Warn("telegram send error message failed", "error", sendErr)
				}
				continue
			}

			if resp.Content == "" {
				continue
			}

			if sendErr := client.sendMessage(ctx, chatID, resp.Content); sendErr != nil {
				if ctx.Err() != nil {
					return
				}
				t.logger.Error("telegram send response failed", "error", sendErr, "chat_id", chatID)
			}
		}
	}
}

// notificationLoop subscribes to TaskNotifyChat events on the bus and delivers
// them to all active Telegram sessions.
func (t *TelegramChannel) notificationLoop(
	ctx context.Context,
	client *telegramClient,
	allowed map[int64]bool,
) {
	if t.eventBus == nil {
		return
	}

	ch := t.eventBus.Subscribe(bus.TaskNotifyChat, bus.MessageResponded)
	defer t.eventBus.Unsubscribe(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			switch evt.Type {
			case bus.TaskNotifyChat:
				result, _ := evt.Payload["result"].(string)
				t.deliverNotification(ctx, client, allowed, result)
			case bus.MessageResponded:
				evtChannel, _ := evt.Payload["channel"].(string)
				if evtChannel == "telegram" {
					break // Don't echo Telegram's own responses.
				}
				sk, _ := evt.Payload["session_key"].(string)
				content, _ := evt.Payload["content"].(string)
				if content == "" || !strings.HasPrefix(sk, "telegram:") {
					break // Only forward responses for Telegram sessions.
				}
				rawID := strings.TrimPrefix(sk, "telegram:")
				chatID, err := strconv.ParseInt(rawID, 10, 64)
				if err != nil {
					break
				}
				if len(allowed) > 0 && !allowed[chatID] {
					break
				}
				if sendErr := client.sendMessage(ctx, chatID, content); sendErr != nil {
					if ctx.Err() != nil {
						return
					}
					t.logger.Error("telegram forward response failed", "chat_id", chatID, "error", sendErr)
				}
			}
		}
	}
}

// deliverNotification persists content as a system message in every active
// Telegram session and sends it via the Bot API.
func (t *TelegramChannel) deliverNotification(
	ctx context.Context,
	client *telegramClient,
	allowed map[int64]bool,
	content string,
) {
	if t.sessions == nil {
		return
	}

	active, err := t.sessions.GetActiveSessions("")
	if err != nil {
		t.logger.Error("telegram deliver notification: get active sessions failed", "error", err)
		return
	}

	for _, sess := range active {
		if sess.Channel != "telegram" {
			continue
		}

		// Parse the chat ID from the session key ("telegram:<chat_id>").
		parts := strings.Split(sess.Key, ":")
		rawID := parts[len(parts)-1]
		chatID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			t.logger.Warn("telegram deliver notification: invalid chat id in session key",
				"session_key", sess.Key, "error", err)
			continue
		}

		if len(allowed) > 0 && !allowed[chatID] {
			continue
		}

		if _, err := t.sessions.AddMessage(sess.Key, session.Message{
			Role:    "system",
			Content: content,
		}); err != nil {
			t.logger.Warn("telegram deliver notification: add message failed",
				"session_key", sess.Key, "error", err)
		}

		if sendErr := client.sendMessage(ctx, chatID, content); sendErr != nil {
			if ctx.Err() != nil {
				return
			}
			t.logger.Error("telegram deliver notification: send failed",
				"chat_id", chatID, "error", sendErr)
		}
	}
}

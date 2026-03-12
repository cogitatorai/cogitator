package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

// ReflectionTrigger monitors sessions and emits reflection.triggered events
// based on message count thresholds and idle timeouts.
type ReflectionTrigger struct {
	eventBus      *bus.Bus
	msgThreshold  int
	idleTimeout   time.Duration
	checkInterval time.Duration
	logger        *slog.Logger
	cancel        context.CancelFunc

	mu       sync.Mutex
	sessions map[string]*sessionTracker
}

type sessionTracker struct {
	messagesSinceReflection int
	lastActivity            time.Time
	lastReflection          time.Time
}

// TriggerConfig configures the ReflectionTrigger.
type TriggerConfig struct {
	EventBus      *bus.Bus
	MsgThreshold  int           // messages before triggering reflection; default 5
	IdleTimeout   time.Duration // idle time before triggering reflection; default 1 hour
	CheckInterval time.Duration // how often to check for idle sessions; default 5 minutes
	Logger        *slog.Logger
}

func NewReflectionTrigger(cfg TriggerConfig) *ReflectionTrigger {
	if cfg.MsgThreshold <= 0 {
		cfg.MsgThreshold = 5
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = time.Hour
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 5 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &ReflectionTrigger{
		eventBus:      cfg.EventBus,
		msgThreshold:  cfg.MsgThreshold,
		idleTimeout:   cfg.IdleTimeout,
		checkInterval: cfg.CheckInterval,
		logger:        cfg.Logger,
		sessions:      make(map[string]*sessionTracker),
	}
}

func (rt *ReflectionTrigger) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	rt.cancel = cancel

	ch := rt.eventBus.Subscribe(bus.MessageReceived)

	// Message count trigger goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				sessionKey, _ := evt.Payload["session_key"].(string)
				if sessionKey == "" {
					continue
				}
				rt.trackMessage(sessionKey)
			}
		}
	}()

	// Idle timeout checker goroutine
	go func() {
		ticker := time.NewTicker(rt.checkInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rt.checkIdleSessions()
			}
		}
	}()
}

func (rt *ReflectionTrigger) Stop() {
	if rt.cancel != nil {
		rt.cancel()
	}
}

func (rt *ReflectionTrigger) trackMessage(sessionKey string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	tracker, ok := rt.sessions[sessionKey]
	if !ok {
		tracker = &sessionTracker{}
		rt.sessions[sessionKey] = tracker
	}

	tracker.lastActivity = time.Now()
	tracker.messagesSinceReflection++

	if tracker.messagesSinceReflection >= rt.msgThreshold {
		tracker.messagesSinceReflection = 0
		tracker.lastReflection = time.Now()
		rt.emitReflection(sessionKey)
	}
}

func (rt *ReflectionTrigger) checkIdleSessions() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	for key, tracker := range rt.sessions {
		if tracker.messagesSinceReflection > 0 &&
			now.Sub(tracker.lastActivity) >= rt.idleTimeout &&
			now.Sub(tracker.lastReflection) >= rt.idleTimeout {
			tracker.messagesSinceReflection = 0
			tracker.lastReflection = now
			rt.emitReflection(key)
		}
	}
}

func (rt *ReflectionTrigger) emitReflection(sessionKey string) {
	rt.eventBus.Publish(bus.Event{
		Type: bus.ReflectionTriggered,
		Payload: map[string]any{
			"session_key": sessionKey,
		},
	})
	rt.logger.Info("reflection triggered", "session_key", sessionKey)
}

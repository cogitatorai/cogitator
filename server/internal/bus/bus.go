package bus

import (
	"sync"
	"time"
)

type EventType string

const (
	MessageReceived     EventType = "message.received"
	MessageResponded    EventType = "message.responded"
	TaskCreated         EventType = "task.created"
	TaskStarted         EventType = "task.started"
	TaskCompleted       EventType = "task.completed"
	TaskFailed          EventType = "task.failed"
	TaskCancelled       EventType = "task.cancelled"
	ReflectionTriggered EventType = "reflection.triggered"
	EnrichmentQueued    EventType = "enrichment.queued"
	ProfileRevisionDue  EventType = "profile.revision.due"
	SkillInstalled      EventType = "skill.installed"
	SkillSearched       EventType = "skill.searched"
	TaskNotifyChat      EventType = "task.notify_chat"
	AgentThinking       EventType = "agent.thinking"
	AgentToolCalling    EventType = "agent.tool_calling"
	SessionTitleSet     EventType = "session.title_set"
	SessionDeleted      EventType = "session.deleted"
	TaskChanged              EventType = "task.changed"
	MCPServerStateChanged    EventType = "mcp.server_state_changed"
	SettingsChanged          EventType = "settings.changed"
	NotificationsRead        EventType = "notifications.read"
	AgentCancelled           EventType = "agent.cancelled"
	UserNotification         EventType = "user.notification"
	VoiceAudioStart          EventType = "voice.audio.start"
	VoiceAudioChunk          EventType = "voice.audio.chunk"
	VoiceAudioEnd            EventType = "voice.audio.end"
	VoiceError               EventType = "voice.error"
	VoiceCancel              EventType = "voice.cancel"
)

type Event struct {
	Type      EventType
	Payload   map[string]any
	Timestamp time.Time
}

type subscriber struct {
	ch    chan Event
	types map[EventType]bool
}

type Bus struct {
	mu          sync.RWMutex
	subscribers []*subscriber
	closed      bool
}

func New() *Bus {
	return &Bus{}
}

func (b *Bus) Subscribe(types ...EventType) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	typeMap := make(map[EventType]bool, len(types))
	for _, t := range types {
		typeMap[t] = true
	}

	sub := &subscriber{
		ch:    make(chan Event, 64),
		types: typeMap,
	}
	b.subscribers = append(b.subscribers, sub)
	return sub.ch
}

func (b *Bus) SubscribeWithBuffer(bufSize int, types ...EventType) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	typeMap := make(map[EventType]bool, len(types))
	for _, t := range types {
		typeMap[t] = true
	}

	sub := &subscriber{
		ch:    make(chan Event, bufSize),
		types: typeMap,
	}
	b.subscribers = append(b.subscribers, sub)
	return sub.ch
}

func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, sub := range b.subscribers {
		if sub.ch == ch {
			close(sub.ch)
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

func (b *Bus) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, sub := range b.subscribers {
		if sub.types[evt.Type] {
			select {
			case sub.ch <- evt:
			default:
				// drop if buffer full
			}
		}
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	for _, sub := range b.subscribers {
		close(sub.ch)
	}
	b.subscribers = nil
}

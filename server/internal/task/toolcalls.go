package task

import (
	"context"
	"sync"
	"time"
)

// ToolCallRecord captures a single tool invocation during a task run.
type ToolCallRecord struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Duration  int64  `json:"duration_ms"`
	Round     int    `json:"round"`
}

// ToolCallCollector accumulates tool call records. It is safe for concurrent use.
type ToolCallCollector struct {
	mu      sync.Mutex
	records []ToolCallRecord
}

// Record appends a tool call record.
func (c *ToolCallCollector) Record(tool, arguments, result string, dur time.Duration, round int, err error) {
	rec := ToolCallRecord{
		Tool:      tool,
		Arguments: arguments,
		Result:    result,
		Duration:  dur.Milliseconds(),
		Round:     round,
	}
	if err != nil {
		rec.Error = err.Error()
	}
	c.mu.Lock()
	c.records = append(c.records, rec)
	c.mu.Unlock()
}

// Records returns a copy of the collected records.
func (c *ToolCallCollector) Records() []ToolCallRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ToolCallRecord, len(c.records))
	copy(out, c.records)
	return out
}

// HasFailures reports whether any recorded tool call resulted in an error.
func (c *ToolCallCollector) HasFailures() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if r.Error != "" {
			return true
		}
	}
	return false
}

// NewCollectorFromRecords reconstructs a ToolCallCollector from persisted
// records, allowing self-healing to operate on historical runs.
func NewCollectorFromRecords(records []ToolCallRecord) *ToolCallCollector {
	return &ToolCallCollector{records: records}
}

type contextKey struct{}

// WithToolCallCollector returns a context carrying the given collector.
func WithToolCallCollector(ctx context.Context, c *ToolCallCollector) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// ToolCallCollectorFromContext extracts the collector from the context, or nil.
func ToolCallCollectorFromContext(ctx context.Context) *ToolCallCollector {
	c, _ := ctx.Value(contextKey{}).(*ToolCallCollector)
	return c
}

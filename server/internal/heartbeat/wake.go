package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// NotifyWakeTime tells the orchestrator when this tenant machine needs to
// be woken for its next cron task. The orchestrator uses this to schedule
// a Fly Machine start at the right moment, enabling scale-to-zero without
// missing scheduled work.
func NotifyWakeTime(orchestratorURL, tenantID, secret string, wakeAt time.Time) error {
	payload, _ := json.Marshal(map[string]any{
		"tenant_id": tenantID,
		"wake_at":   wakeAt.UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequest("POST", orchestratorURL+"/api/internal/schedule-wake", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", secret)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notifying wake time: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("orchestrator returned status %d", resp.StatusCode)
	}
	return nil
}

package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// NotifyStatus tells the orchestrator that this tenant's status has changed.
func NotifyStatus(orchestratorURL, tenantID, secret, status string) error {
	payload, _ := json.Marshal(map[string]string{
		"tenant_id": tenantID,
		"status":    status,
	})
	req, err := http.NewRequest("POST", orchestratorURL+"/api/internal/tenant-status", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", secret)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notifying status: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("orchestrator returned status %d", resp.StatusCode)
	}
	return nil
}

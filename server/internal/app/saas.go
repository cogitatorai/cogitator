//go:build saas

package app

import (
	"fmt"
	"os"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

const isSaaS = true

// buildSaaSProvider creates a provider that routes through the orchestrator's LLM proxy.
// Returns an error if required environment variables are missing.
func buildSaaSProvider() (provider.Provider, error) {
	orchestratorURL := os.Getenv("COGITATOR_ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		return nil, fmt.Errorf("COGITATOR_ORCHESTRATOR_URL is required in SaaS mode")
	}
	tenantID := os.Getenv("COGITATOR_TENANT_ID")
	if tenantID == "" {
		return nil, fmt.Errorf("COGITATOR_TENANT_ID is required in SaaS mode")
	}
	internalSecret := os.Getenv("COGITATOR_INTERNAL_SECRET")

	p := provider.NewOpenAI(orchestratorURL+"/api/internal/llm/v1", "")
	p.ExtraHeaders = map[string]string{
		"X-Tenant-ID":       tenantID,
		"X-Internal-Secret": internalSecret,
	}
	return p, nil
}

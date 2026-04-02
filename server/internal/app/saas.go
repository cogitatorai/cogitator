//go:build saas

package app

import (
	"os"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

const defaultSaaSDashboardDir = "/data/public"
const isSaaS = true

// buildSaaSProvider creates a provider that routes through the orchestrator's LLM proxy.
func buildSaaSProvider() provider.Provider {
	orchestratorURL := os.Getenv("COGITATOR_ORCHESTRATOR_URL")
	tenantID := os.Getenv("COGITATOR_TENANT_ID")
	internalSecret := os.Getenv("COGITATOR_INTERNAL_SECRET")

	p := provider.NewOpenAI(orchestratorURL+"/api/internal/llm/v1", "")
	p.ExtraHeaders = map[string]string{
		"X-Tenant-ID":       tenantID,
		"X-Internal-Secret": internalSecret,
	}
	return p
}

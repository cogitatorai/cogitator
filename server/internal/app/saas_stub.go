//go:build !saas

package app

import "github.com/cogitatorai/cogitator/server/internal/provider"

const defaultSaaSDashboardDir = ""
const isSaaS = false

func buildSaaSProvider() provider.Provider { return nil }

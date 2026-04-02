//go:build !saas

package app

import "github.com/cogitatorai/cogitator/server/internal/provider"

const isSaaS = false

func buildSaaSProvider() (provider.Provider, error) { return nil, nil }

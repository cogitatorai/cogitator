package orchestrator

import "os"

// Config holds all configuration for the orchestrator service.
type Config struct {
	Port                string
	DBPath              string
	FlyAPIToken         string
	FlyAppName          string
	CloudflareToken     string
	CloudflareZoneID    string
	InternalSecret      string
	JWTSecret           string
	StripeKey           string
	StripeWebhookSecret string
}

// LoadConfig reads orchestrator configuration from environment variables.
func LoadConfig() Config {
	return Config{
		Port:                envOr("PORT", "8485"),
		DBPath:              envOr("ORCHESTRATOR_DB_PATH", "/data/orchestrator.db"),
		FlyAPIToken:         os.Getenv("FLY_API_TOKEN"),
		FlyAppName:          envOr("FLY_APP_NAME", "cogitator-saas"),
		CloudflareToken:     os.Getenv("CLOUDFLARE_API_TOKEN"),
		CloudflareZoneID:    os.Getenv("CLOUDFLARE_ZONE_ID"),
		InternalSecret:      os.Getenv("COGITATOR_INTERNAL_SECRET"),
		JWTSecret:           os.Getenv("ORCHESTRATOR_JWT_SECRET"),
		StripeKey:           os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

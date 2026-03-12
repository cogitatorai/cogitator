package connector

// Build-time credentials injected via ldflags.
// Example: -X github.com/cogitatorai/cogitator/server/internal/connector.GoogleClientID=...
var (
	GoogleClientID     string
	GoogleClientSecret string
)

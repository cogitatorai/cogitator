package mcp

import "strings"

// EnrichToolDescription prefixes a tool's description with server context.
// Format: "[ServerName: Instructions] Description" or "[ServerName] Description".
func EnrichToolDescription(serverName, instructions, description string) string {
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(serverName)
	if instructions != "" {
		b.WriteString(": ")
		b.WriteString(instructions)
	}
	b.WriteByte(']')
	if description != "" {
		b.WriteByte(' ')
		b.WriteString(description)
	}
	return b.String()
}

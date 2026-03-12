package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateSkillContent produces a SKILL.md body for an MCP server.
// The content describes the server's purpose and catalogs its tools
// with descriptions and input schemas so the agent knows how to use them.
func GenerateSkillContent(serverName, instructions string, tools []ToolInfo) string {
	var b strings.Builder

	// Frontmatter
	fmt.Fprintf(&b, "---\nname: mcp-%s\n", slugify(serverName))
	fmt.Fprintf(&b, "description: MCP server %q tools and usage guide.\n", serverName)
	b.WriteString("---\n\n")

	// Header
	fmt.Fprintf(&b, "# %s (MCP Server)\n\n", serverName)

	if instructions != "" {
		b.WriteString(instructions)
		b.WriteString("\n\n")
	}

	// Tool catalog
	fmt.Fprintf(&b, "## Tools (%d)\n\n", len(tools))

	for _, t := range tools {
		fmt.Fprintf(&b, "### %s\n\n", t.QualifiedName)
		if t.Description != "" {
			b.WriteString(t.Description)
			b.WriteString("\n\n")
		}

		// Render input schema as JSON for the agent.
		if len(t.InputSchema) > 0 {
			b.WriteString("**Input schema:**\n\n```json\n")
			schemaJSON, err := json.MarshalIndent(t.InputSchema, "", "  ")
			if err == nil {
				b.Write(schemaJSON)
			}
			b.WriteString("\n```\n\n")
		}
	}

	return b.String()
}

// slugify converts a server name to a URL/filesystem-safe slug.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-' || r == '.':
			b.WriteByte('-')
		}
	}
	// Trim leading/trailing dashes and collapse consecutive dashes.
	s := b.String()
	var clean strings.Builder
	prev := byte('-')
	for i := 0; i < len(s); i++ {
		if s[i] == '-' && prev == '-' {
			continue
		}
		clean.WriteByte(s[i])
		prev = s[i]
	}
	return strings.Trim(clean.String(), "-")
}

// MCPSkillSlug returns the skill slug for an MCP server name.
func MCPSkillSlug(serverName string) string {
	return "mcp-" + slugify(serverName)
}

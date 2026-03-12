package tools

import (
	"sort"
	"strings"
)

// ResolvedTools holds pre-categorized, display-ready tool activity.
// Nil/omitted when no tools were used.
type ResolvedTools struct {
	Skills []string `json:"skills,omitempty"`
	Tools  []string `json:"tools,omitempty"`
	Memory []string `json:"memory,omitempty"`
}

// builtinDisplayNames maps internal tool names to human-readable labels.
var builtinDisplayNames = map[string]string{
	"shell":          "Shell",
	"read_file":      "Read file",
	"write_file":     "Write file",
	"list_directory": "List directory",
	"create_task":    "Create task",
	"list_tasks":     "List tasks",
	"run_task":       "Run task",
	"delete_task":    "Delete task",
	"toggle_task":    "Toggle task",
	"heal_task":      "Heal task",
	"allow_domain":   "Allow domain",
	"fetch_url":        "Fetch URL",
	"web_search":       "Web search",
	"start_mcp_server": "Start MCP server",
}

// skillDisplayNames maps skill-related tool names to human-readable labels.
var skillDisplayNames = map[string]string{
	"read_skill":           "Read skill",
	"search_skills":        "Search skills",
	"install_skill":        "Install skill",
	"list_installed_skills": "List skills",
	"create_skill":         "Create skill",
	"update_skill":         "Update skill",
}

// Resolve categorizes raw tool names into display-ready groups.
// It uses the registry to look up MCP server names for qualified tool names.
func Resolve(names []string, registry *Registry) ResolvedTools {
	if len(names) == 0 {
		return ResolvedTools{}
	}

	skillSet := make(map[string]struct{})
	toolSet := make(map[string]struct{})
	memorySet := make(map[string]struct{})

	for _, name := range names {
		if name == "save_memory" {
			memorySet["Saved memory"] = struct{}{}
			continue
		}

		if label, ok := skillDisplayNames[name]; ok {
			skillSet[label] = struct{}{}
			continue
		}

		if label, ok := builtinDisplayNames[name]; ok {
			toolSet[label] = struct{}{}
			continue
		}

		// MCP tools: look up the server name from the registry first.
		if strings.HasPrefix(name, "mcp__") {
			serverName := ""
			if registry != nil {
				if def, ok := registry.Get(name); ok && def.MCPServer != "" {
					serverName = def.MCPServer
				}
			}
			// Fallback: parse server name from the qualified name pattern mcp__X__Y.
			if serverName == "" {
				parts := strings.SplitN(name, "__", 3)
				if len(parts) >= 2 {
					serverName = parts[1]
				}
			}
			if serverName != "" {
				skillSet[serverName+" (MCP)"] = struct{}{}
			}
			continue
		}

		// Unknown tool: show as-is under Tools.
		toolSet[name] = struct{}{}
	}

	return ResolvedTools{
		Skills: sortedKeys(skillSet),
		Tools:  sortedKeys(toolSet),
		Memory: sortedKeys(memorySet),
	}
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

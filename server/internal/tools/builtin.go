package tools

// registerBuiltinTools populates r with the standard built-in tool definitions.
// These provide the LLM with function-calling schemas for core workspace operations.
func registerBuiltinTools(r *Registry) {
	builtins := []ToolDef{
		{
			Name:        "read_file",
			Description: "Read the contents of a file from the workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Relative path to the file",
					},
				},
				"required": []string{"path"},
			},
			Builtin: true,
		},
		{
			Name:        "write_file",
			Description: "Write content to a file in the workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Relative path to the file",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content to write",
					},
				},
				"required": []string{"path", "content"},
			},
			Builtin: true,
		},
		{
			Name:        "list_directory",
			Description: "List files and directories in a directory",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Relative path to the directory",
					},
				},
				"required": []string{"path"},
			},
			Builtin: true,
		},
		{
			Name:        "shell",
			Description: "Execute a shell command in the workspace",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The command to execute",
					},
				},
				"required": []string{"command"},
			},
			Builtin: true,
		},
		{
			Name:        "create_task",
			Description: "Create a scheduled task that runs automatically on a cron schedule. Use this when the user asks you to do something recurring (e.g. daily weather reports, periodic checks). IMPORTANT: Before creating, call list_tasks to check for an existing task with the same purpose. To modify an existing task, use update_task instead of deleting and recreating it. Before creating a task, follow this skill resolution chain: (1) Check installed skills (list_installed_skills). (2) If none match, search ClawHub (search_skills) and try candidates in order. For each candidate, call install_skill. If the response is domain_approval_required, present the required_domains to the user and ask if they approve adding them to the allowlist. If the user approves, retry with force=true. If the user denies, skip that skill and try the next search result. (3) After 3 consecutive domain denials, stop trying ClawHub skills and create your own skill (create_skill) with proper OpenClaw format. (4) If no ClawHub results match at all, create your own skill (create_skill). The task prompt MUST reference the skill via read_skill rather than calling raw APIs directly.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Short, descriptive name for the task (e.g. 'daily-weather-report')",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "A direct action prompt that will be executed by an LLM each time the task runs. Write it as an instruction to perform the action NOW, not to schedule it. The prompt MUST begin with 'First, read the skill <node_id> using read_skill' if an installed skill is available for the task, so the executing agent loads the skill instructions before acting. Good: 'First, read skill 01KJA8FAXW using read_skill to load the weather instructions, then fetch the current weather for Meudon, France and summarize the forecast.' Bad: 'Fetch the weather using wttr.in.' The prompt must never ask to create, schedule, or manage tasks.",
					},
					"cron_expr": map[string]any{
						"type":        "string",
						"description": "Cron expression for the schedule (e.g. '0 9 * * *' for 9am daily, '*/30 * * * *' for every 30 minutes). Uses standard 5-field cron format: minute hour day-of-month month day-of-week. Times are in server local time. Check memory for the user's timezone; if known, silently convert to server time without asking. Never ask the user for their timezone if you already have it in memory.",
					},
					"model_tier": map[string]any{
						"type":        "string",
						"description": "Which model to use: 'standard' for complex tasks, 'cheap' for simple ones. Defaults to 'cheap'.",
						"enum":        []string{"standard", "cheap"},
					},
					"notify_chat": map[string]any{
						"type":        "boolean",
						"description": "Whether to send the task result to the user's active chat session. Defaults to true. Set to true when the user expects to receive the output (e.g. 'give me a daily forecast', 'send me a summary', 'tell me the status'). Set to false for background tasks where the user only cares about logging (e.g. 'clean up old files nightly', 'rotate logs every hour').",
					},
					"notify_users": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
						"description": "List of user names to notify when the task completes. Use exact names from list_users. Use [\"everyone\"] to notify all users. Omit to notify only the task owner.",
					},
				},
				"required": []string{"name", "prompt", "cron_expr"},
			},
			Builtin: true,
		},
		{
			Name:        "update_task",
			Description: "Update an existing scheduled task in place. Only the fields you provide will be changed; omitted fields are left untouched. Use this instead of deleting and recreating a task when you only need to change the prompt, schedule, model tier, or notification setting.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "The ID of the task to update (from list_tasks results)",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "New prompt for the task. Omit to keep the current prompt.",
					},
					"cron_expr": map[string]any{
						"type":        "string",
						"description": "New cron expression. Omit to keep the current schedule.",
					},
					"model_tier": map[string]any{
						"type":        "string",
						"description": "Which model to use: 'standard' or 'cheap'. Omit to keep the current tier.",
						"enum":        []string{"standard", "cheap"},
					},
					"notify_chat": map[string]any{
						"type":        "boolean",
						"description": "Whether to send results to the user's chat. Omit to keep the current setting.",
					},
					"notify_users": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
						"description": "List of user names to notify on completion. Use [\"everyone\"] for all users. Omit to keep the current setting.",
					},
				},
				"required": []string{"task_id"},
			},
			Builtin: true,
		},
		{
			Name:        "list_tasks",
			Description: "List all scheduled tasks and their status.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
			Builtin: true,
		},
		{
			Name:        "delete_task",
			Description: "Delete a scheduled task permanently. Use list_tasks first to find the task ID. Always confirm with the user before deleting.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "The ID of the task to delete (from list_tasks results)",
					},
				},
				"required": []string{"task_id"},
			},
			Builtin: true,
		},
		{
			Name:        "toggle_task",
			Description: "Enable or disable a scheduled task without deleting it. A disabled task stays in the list but will not run on its cron schedule. Use list_tasks first to find the task ID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "The ID of the task to enable or disable (from list_tasks results)",
					},
					"enabled": map[string]any{
						"type":        "boolean",
						"description": "True to enable the task, false to disable it",
					},
				},
				"required": []string{"task_id", "enabled"},
			},
			Builtin: true,
		},
		{
			Name:        "run_task",
			Description: "Manually trigger execution of a scheduled task right now. Use this when the user wants to run a task immediately instead of waiting for its cron schedule, or to retry a failed task. Use list_tasks first to find the task ID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "The ID of the task to run (from list_tasks results)",
					},
				},
				"required": []string{"task_id"},
			},
			Builtin: true,
		},
		{
			Name:        "heal_task",
			Description: "Diagnose and fix a task based on its last run. Analyzes tool call logs and output, then corrects the skill or task prompt so future runs succeed. Works for both erroring tasks and tasks that succeed but produce wrong results.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "The ID of the task to heal (from list_tasks results)",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Why the task needs healing, e.g. 'the digest was empty' or 'the result contained stale data'. When omitted, healing only runs if the last run had tool call failures.",
					},
				},
				"required": []string{"task_id"},
			},
			Builtin: true,
		},
		{
			Name:        "search_skills",
			Description: "Search ClawHub for installable skills by keyword. IMPORTANT: Before searching, always call list_installed_skills first to check if a matching skill is already available. Only search if no installed skill matches your need.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query describing the capability you need (e.g. 'weather', 'web search', 'image generation')",
					},
				},
				"required": []string{"query"},
			},
			Builtin: true,
		},
		{
			Name:        "install_skill",
			Description: "Install a skill from ClawHub by its slug. The skill content is automatically scanned for security issues before installation. If suspicious patterns are found (status='review_required'), present all warnings to the user and only retry with force=true if they explicitly approve. If the skill requires network access to external domains not yet allowed (status='domain_approval_required'), present the required_domains list to the user and ask for approval. If the user approves, retry with force=true to install and allowlist the domains. If the user denies, do NOT install this skill; try the next candidate from search results instead. Do NOT install a skill that is already installed (installed=true in search results). Not available during task execution.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "The skill slug (e.g. 'weather') or a ClawHub URL (e.g. 'https://clawhub.ai/skills/weather')",
					},
					"force": map[string]any{
						"type":        "boolean",
						"description": "Set to true only after the user has reviewed security warnings and explicitly approved installation. Never set this without user consent.",
					},
				},
				"required": []string{"slug"},
			},
			Builtin: true,
		},
		{
			Name:        "create_skill",
			Description: "Create a new skill ONLY when no suitable skill exists (neither installed nor on ClawHub). IMPORTANT: Always call list_installed_skills first. If a skill with the same purpose already exists, use update_skill instead of creating a duplicate. The content MUST be a valid OpenClaw SKILL.md: YAML frontmatter (name, description) followed by markdown instructions. Structure the body with: overview, quick reference table, detailed usage sections (one H2 per capability, self-contained), parameter tables, fenced code blocks for all commands/URLs, and error handling. Write for an LLM reader: be precise, use imperative mood, no preambles or marketing. Keep under 2500 words. The skill is saved locally and can be used immediately via read_skill. IMPORTANT: When the skill content references API endpoints, use real API URLs only. Do NOT include placeholder domains (example.com, test.com, etc.) because all domains in the skill content are automatically added to the network allowlist. Only include domains the skill actually needs to contact.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "Lowercase, hyphenated skill name (e.g. 'open-meteo-weather', 'github-issues')",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Human-readable display name (e.g. 'Open-Meteo Weather')",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "One to two sentence description starting with a verb. 20-80 words.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The full SKILL.md content including YAML frontmatter (---\\nname: ...\\ndescription: ...\\n---) and markdown body with structured instructions.",
					},
				},
				"required": []string{"slug", "name", "summary", "content"},
			},
			Builtin: true,
		},
		{
			Name:        "list_installed_skills",
			Description: "List all installed skills (from ClawHub and self-created). Always call this before searching for or installing new skills to avoid duplicates.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
			Builtin: true,
		},
		{
			Name:        "read_skill",
			Description: "Read the content of an installed skill to understand how to use it. Returns the skill's SKILL.md with usage instructions, example commands, and API details.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]any{
						"type":        "string",
						"description": "The memory node ID of the installed skill (from install_skill or list_installed_skills results)",
					},
				},
				"required": []string{"node_id"},
			},
			Builtin: true,
		},
		{
			Name:        "update_skill",
			Description: "Update the SKILL.md content of an installed skill. Use this in two situations: (1) The user asks to modify, fix, or improve a skill. (2) During task execution, when a skill's instructions produce errors, wrong output, or broken commands, and you discover a working alternative. In case (2), you MUST update the skill immediately so future runs succeed without retries; do not wait for the user to ask. Always prefer this over create_skill when a skill with the same purpose already exists. Call list_installed_skills and read_skill first to get the current content and node_id. Only change the parts that need updating; preserve the rest of the skill content.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]any{
						"type":        "string",
						"description": "The memory node ID of the skill to update (from read_skill or list_installed_skills results)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The full updated SKILL.md content including YAML frontmatter and markdown body",
					},
				},
				"required": []string{"node_id", "content"},
			},
			Builtin: true,
		},
		{
			Name:        "allow_domain",
			Description: "Add a domain to the network security allowlist so that network commands (curl, wget, etc.) can access it. Use this after the user explicitly approves a domain that was blocked. IMPORTANT: Always confirm with the user before calling this tool. Never allowlist a domain without explicit user consent.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"domain": map[string]any{
						"type":        "string",
						"description": "The domain to allow (e.g. 'api.coingecko.com', '*.open-meteo.com'). Supports exact domains and wildcard prefixes.",
					},
				},
				"required": []string{"domain"},
			},
			Builtin: true,
		},
		{
			Name:        "start_mcp_server",
			Description: "Start an MCP server that is currently stopped so its tools become available. Call this when you see an MCP server listed in your system prompt with status \"stopped\" and you need to use its tools. After a successful start, the server's tools will be available in subsequent tool calls within the same conversation turn.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_name": map[string]any{
						"type":        "string",
						"description": "The name of the MCP server to start (from the MCP server list in the system prompt)",
					},
				},
				"required": []string{"server_name"},
			},
			Builtin: true,
		},
		{
			Name:        "list_users",
			Description: "List the other people who use this app. Returns their names and IDs. Use this when the user asks about other household members or when you need to know who else is on this instance. Use the returned ID when saving memories about a specific person (subject_id parameter of save_memory).",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Builtin: true,
		},
		{
			Name:        "notify_user",
			Description: "Send a notification message to another user on this platform. The message will appear in their Tasks notification list with your name as the sender. Use exact names as returned by list_users.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_name": map[string]any{
						"type":        "string",
						"description": "The exact display name of the user to notify (from list_users)",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "The message to send to the user",
					},
				},
				"required": []string{"user_name", "message"},
			},
			Builtin: true,
		},
		{
			Name:        "toggle_memory_privacy",
			Description: "Toggle a memory between private and shared. Private memories are only visible to the user who owns them. Shared memories are visible to all users. Use this when a user asks to make a memory private or share it with everyone.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{
						"type":        "string",
						"description": "The ID of the memory node to toggle (from retrieved memories context)",
					},
					"private": map[string]any{
						"type":        "boolean",
						"description": "True to make the memory private (visible only to you), false to share it with all users",
					},
				},
				"required": []string{"memory_id", "private"},
			},
			Builtin: true,
		},
		{
			Name:        "save_memory",
			Description: "Save information to long-term memory so you can recall it in future conversations. Use this whenever the user shares something worth remembering: timezone, preferences, names, locations, habits, corrections. Always write content in English regardless of the conversation language. When saving information about someone other than the current user, include that person's name in the content and set subject_id to their user ID (from list_users). Each memory must be atomic: if the user lists multiple items, call save_memory once per item so each is stored and retrievable independently.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "Short, descriptive title in English (e.g. 'User timezone is Europe/Paris'). Optional; auto-generated from content if omitted.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The full content to remember, written in English. Be specific and self-contained so the memory is useful without additional context.",
					},
					"pinned": map[string]any{
						"type":        "boolean",
						"description": "Set to true for core identity facts (name, timezone, language), persistent preferences (dietary restrictions, communication style), and other knowledge that should always be present in context. Defaults to false.",
					},
					"subject_id": map[string]any{
						"type":        "string",
						"description": "The user ID of the person this memory is about, if it is about someone other than the current user. Get this from list_users. This ensures the memory stays correctly linked even if the person changes their name.",
					},
				},
				"required": []string{"content"},
			},
			Builtin: true,
		},
		{
			Name:        "fetch_url",
			Description: "Fetch a web page and return its content as markdown. Use this to read documentation, articles, API responses, or any web content. In interactive chat, any URL is allowed. During autonomous task execution, the domain must be allowlisted first (via allow_domain or skill installation).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to fetch (http or https)",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "Optional CSS selector to extract specific content (e.g. 'main', '.content', '#article'). When omitted, the full page is converted.",
					},
					"raw": map[string]any{
						"type":        "boolean",
						"description": "Return raw HTML instead of markdown. Default: false.",
					},
				},
				"required": []string{"url"},
			},
			Builtin: true,
		},
		{
			Name:        "web_search",
			Description: "Search the web and return a list of results with titles, URLs, and snippets. Use this when you need to find information, look up current events, or discover relevant URLs. Chain with fetch_url to read specific results in detail.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query",
					},
					"count": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 5, max: 10)",
					},
				},
				"required": []string{"query"},
			},
			Builtin: true,
		},
	}

	for _, b := range builtins {
		r.tools[b.Name] = b
	}
}



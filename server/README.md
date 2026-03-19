# Cogitator

**[cogitator.cloud](https://cogitator.cloud)**

A self-learning personal AI agent. Go backend, React dashboard, SQLite storage, extensible through installable skills and custom tools.

Cogitator runs as a daemon on port 8484. You interact with it through a real-time WebSocket chat or by scheduling tasks that run autonomously on cron schedules. It remembers what you tell it, discovers and installs skills from ClawHub, and learns from its own task execution history.

## Architecture

```
cmd/cogitator/        Single binary entry point
internal/
  agent/              LLM agent loop with tool calling and context building
  api/                HTTP/REST API and route handlers
  bus/                In-process event bus for decoupled communication
  channel/            Chat transports (WebSocket, Telegram)
  config/             YAML config with environment variable overrides
  database/           SQLite with WAL mode, auto-migrations
  mcp/                MCP server lifecycle, tool discovery, remote transport
  memory/             Knowledge graph: nodes, edges, vector embeddings, retrieval
  provider/           LLM provider abstraction (OpenAI-compatible)
  sandbox/            Shell execution sandbox (host with env scrubbing, or Docker containers)
  security/           Path filtering, command blocking, domain allowlisting, audit logging
  session/            Conversation session and message persistence
  skills/             ClawHub skill discovery, installation, and management
  task/               Scheduled task execution, runs, cancellation, tool call tracking
  tools/              Built-in and custom tool registry and execution
  auth/               JWT service, refresh tokens, and request context helpers
  user/               User store with CRUD, invite codes, and authentication
  worker/             Background workers: enrichment, profiling, consolidation
  workspace/          File system layout for data, memories, skills, tools
dashboard/            React 19 + Vite 7 + Tailwind CSS 4 SPA
```

## Features

**Chat**: Real-time WebSocket conversation with the agent. Markdown rendering, activity indicators (thinking, tool use), session management with sidebar.

**Memory**: Persistent knowledge graph with vector retrieval. The agent stores facts, preferences, patterns, and episodes using the `save_memory` tool. Memories are embedded as vectors (including node content, up to 100 cross-domain retrieval triggers, and metadata) and retrieved via cosine similarity during conversation. Retrieval is budget-based: instead of a fixed result count, the system fills a configurable token budget (`retrieval_token_budget`, default 2000) with the highest-scoring candidates, so short preferences fill cheaply while long memories consume more budget. Only stable knowledge types (facts, preferences, patterns, skills) enter the candidate pool; episodes are excluded from similarity search but remain reachable via graph edge-following. Preference and fact nodes receive a configurable score boost (`retrieval_type_boost`, default 1.1x) and candidates below a similarity floor (`retrieval_min_similarity`, default 0.3) are filtered out. Critical facts can be pinned so they are always present in the agent's context. A background consolidation worker clusters related nodes into higher-level patterns as the graph grows. The enrichment pipeline generates up to 100 retrieval triggers per node across three categories (direct, contextual, and lateral/cross-domain associations). Bumping `enrichment_version` in the config triggers automatic re-enrichment of all nodes on the next restart. When no embedding model is available, retrieval falls back to LLM-based classification with optional association expansion.

**Tasks**: Autonomous scheduled execution. Tasks run on cron schedules with the full agent loop (including tool access). Features include: manual triggering, concurrent run prevention, cancellation, 10-minute timeout cap, stale run cleanup on restart, tool call tracking, configurable chat output via `notify_chat` (results appear in the pinned Tasks chat session), and broadcast mode to notify all users on completion. Task lifecycle events (completed, failed) trigger a notification badge in the sidebar bell dropdown.

**Skills**: Discover and install capabilities from ClawHub. The agent can search for, install, read, and use skills at runtime. Installed skills are stored as memory nodes with their instructions available for retrieval.

**Tools**: 23 built-in tools plus custom tool support via YAML definitions.

| Tool | Description |
|---|---|
| `read_file` | Read workspace files |
| `write_file` | Write workspace files |
| `list_directory` | List directory contents |
| `shell` | Execute shell commands |
| `create_task` | Create a scheduled task |
| `update_task` | Update an existing task in place (prompt, schedule, model tier, notifications) |
| `list_tasks` | List all tasks |
| `run_task` | Manually trigger a task |
| `delete_task` | Delete a task |
| `toggle_task` | Enable or disable a task |
| `heal_task` | Diagnose and fix a task from its last run |
| `search_skills` | Search ClawHub for skills |
| `install_skill` | Install a skill |
| `create_skill` | Create a custom skill |
| `update_skill` | Update an installed skill |
| `list_installed_skills` | List installed skills |
| `read_skill` | Read skill instructions |
| `save_memory` | Save to long-term memory (supports `subject_id` for person attribution) |
| `list_users` | List other household members with IDs |
| `toggle_memory_privacy` | Toggle a memory between private and shared |
| `allow_domain` | Add a domain to the network allowlist |
| `fetch_url` | Fetch a web page and convert to markdown |
| `web_search` | Search the web via DuckDuckGo, Brave, or Mojeek |
| `start_mcp_server` | Start a configured MCP server by name |

Custom tools are defined as YAML files in the workspace `tools/` directory with a name, description, parameters schema, and shell command template.

**MCP Servers**: Connect external tools via the Model Context Protocol. Add local (stdio) or remote (SSE/Streamable HTTP) MCP servers through the dashboard or API. The agent automatically discovers server tools, enriches their descriptions with server context, and generates a skill per server so it knows when and how to use each tool. Per-server instructions describe the server's purpose and guide tool selection. Remote servers support header-based auth and OAuth. Servers auto-reconnect with exponential backoff on connection loss.

**Multi-user**: JWT-based authentication with role-based access control (admin, moderator, user). Admin credentials are bootstrapped from environment variables on first run. New users register via invite codes generated from the Admin page. Sessions, tasks, memory, and background workers are scoped by user. Private sessions are visible only to their owner. Per-user profile overrides are merged into the agent system prompt. The Users page lets admins view and manage household members.

**Household Awareness**: Cross-user memory sharing for household contexts. The agent can access shared memories from other household members (with privacy controls) so it understands household-wide preferences, routines, and relationships. Shared memory nodes are injected into the agent's context alongside the user's own memories.

**Security**: Credential isolation and sandboxed shell execution. API keys, bot tokens, and OAuth credentials are stored in the OS keychain (with file-based fallback), never written to the main config. Existing `secrets.yaml` files are automatically migrated to the keychain on first startup. Shell commands run in an isolated subdirectory with a scrubbed environment (variables containing API_KEY, SECRET, TOKEN, PASSWORD, etc. are stripped). Sensitive files (`secrets.yaml`, `cogitator.yaml`, `~/.ssh`, `~/.aws`, ...) are blocked from shell access by path filtering. Optional Docker sandboxing runs commands in throwaway containers with memory/PID limits, read-only rootfs, and no network by default. Configure via `security.sandbox` ("auto", "docker", "host"). All API endpoints require JWT authentication. The macOS desktop app injects credentials via the WKWebView for seamless auto-login.

**Connectors**: OAuth-based integrations with external services. Google Calendar and Gmail ship as built-in connectors. The agent calls calendar tools during conversation to check schedules, list events, and search across multiple calendars. Users choose which calendars to include from the settings modal on the Connectors page. The connector runtime supports custom connectors defined as YAML manifests with OAuth config, REST endpoints, and jq-based response mapping. Multi-calendar queries fan out in parallel with deduplication and chronological sorting.

**Dashboard pages**: Chat, Tasks, Memory, Skills, Connectors, MCP Servers, History, Resources, Account, Users (admin-only), Settings, Admin (admin-only).

## Requirements

- Go 1.25+
- Node.js 20+ (for dashboard build)
- An OpenAI-compatible LLM provider

## Quick Start

```sh
# Clone and build
git clone https://github.com/deiu/cogitator.git
cd cogitator
make all

# Configure provider
cp .env.example .env
# Edit .env with your provider API key, or configure via the dashboard Settings page

# Run
./cogitator
```

Open `http://localhost:8484` in your browser.

## Configuration

Cogitator uses a `cogitator.yaml` config file with environment variable overrides. The config is auto-saved to the workspace directory so dashboard settings persist across restarts.

Environment variables can be set in a `.env` file in the project root or exported in the shell. Shell variables take precedence over `.env` values.

```sh
# .env (or export in shell)
COGITATOR_SERVER_PORT=8484
COGITATOR_SERVER_HOST=0.0.0.0
COGITATOR_WORKSPACE_PATH=~/.cogitator
COGITATOR_JWT_SECRET=<random-hex-string>
```

If the configured port is already in use, the server automatically falls back to an OS-assigned port and persists it to `cogitator.yaml` so subsequent launches reuse the same port.

Provider credentials can be set via environment variables or through the dashboard Settings page. Credentials are stored in the OS keychain (macOS Keychain, Windows Credential Manager, Linux secret-service) with automatic fallback to a `secrets.yaml` file when the keychain is unavailable.

**Model tiers**: Tasks and conversations use a two-tier model system (`standard` and `cheap`). Configure each tier with a provider and model name. Tasks default to the `cheap` tier; complex tasks can be set to `standard`.

## Development

```sh
# Install dashboard dependencies
make dashboard-install

# Start backend + dashboard dev server (hot reload)
make dev

# Run Go tests
make test

# Build Go binary only
make build

# Build dashboard only
make dashboard

# Lint
make lint
```

## macOS App

```sh
# Build .app bundle (Apple Silicon)
make app

# Build universal binary (Apple Silicon + Intel)
make app-universal

# Release (builds, signs, notarizes, packages zip + DMG)
make release APPLE_TEAM_ID=YOUR_TEAM_ID VERSION=0.1.0
make release-universal APPLE_TEAM_ID=YOUR_TEAM_ID VERSION=0.1.0
```

The release target produces two artifacts: a zip (used by the auto-updater) and a styled DMG with drag-to-Applications for first-time distribution.

On first launch, the app presents an onboarding screen with two options: "Set up your own Cogitator" (runs a local server) or "Join an existing Cogitator" (connects to a remote instance via invite code). The chosen mode is saved to `~/.cogitator/desktop.json`. Use the "Switch Mode..." menu item to return to onboarding.

**Status bar**: Closing the window (red X or Cmd+W) hides the app to the macOS status bar instead of quitting. The server continues running in the background. Click the status bar icon to reopen the window, or use Cmd+Q to fully quit.

The app targets macOS 13.0+ and is signed with a Developer ID certificate by default. For notarization, store credentials once:

```sh
xcrun notarytool store-credentials cogitator
```

This prompts for your Apple ID, an app-specific password (generate at appleid.apple.com), and your Team ID. Then pass `APPLE_TEAM_ID` to `make release` to enable notarization. Without it, signing still runs but notarization is skipped.

To override the signing identity or notarization profile, create a `.env` file in the project root:

```
CODESIGN_ID=Developer ID Application: Your Name (TEAMID)
APPLE_TEAM_ID=TEAMID
NOTARIZE_PROFILE=cogitator
```

**Auto-update**: The app checks GitHub releases every 30 minutes. When a newer version is found, a banner appears in the dashboard. The last check result is cached, so the banner appears instantly on restart without waiting for a GitHub call. Clicking "Update Now" downloads the new bundle, swaps it in place, and relaunches. Click "Skip" to dismiss the banner for that version; a newer release will bring the banner back. For private repositories, add a GitHub token to `secrets.yaml`:

```yaml
github:
  token: ghp_xxxxxxxxxxxx
```

## Mobile App

React Native (Expo SDK 54) companion app for iOS and Android. Connects to the same WebSocket and REST endpoints as the dashboard.

```sh
cd mobile
make install          # npm ci
make start            # Expo dev server
make testflight       # Build iOS locally and upload to TestFlight
```

The `testflight` target runs the full pipeline on your Mac: expo prebuild, xcodebuild archive, sign with automatic provisioning, and upload directly to App Store Connect. No EAS or cloud build service required.

## Docker

```sh
# Build image
make docker

# Run with docker-compose
docker-compose up -d
```

The container exposes port 8484 and persists data to a `/data` volume. Configure via `.env` file or environment variables.

## API

All endpoints are prefixed with `/api`. The WebSocket endpoint is at `/ws`.

| Method | Path | Description |
|---|---|---|
| GET | `/api/auth/needs-setup` | Check if first-run setup is required |
| POST | `/api/auth/setup` | Create initial admin account (first run only) |
| POST | `/api/auth/register` | Register with invite code |
| POST | `/api/auth/login` | Login (returns JWT + refresh token) |
| POST | `/api/auth/refresh` | Refresh access token |
| POST | `/api/auth/logout` | Revoke refresh token |
| POST | `/api/auth/social` | Social sign-in (Google/Apple ID token) |
| GET | `/api/auth/providers` | Available social auth providers |
| GET | `/api/auth/google/start` | Start Google OAuth flow |
| GET | `/api/auth/google/callback` | Google OAuth callback |
| GET | `/api/auth/claim/{id}` | Claim tokens from OAuth flow |
| GET | `/api/auth/me` | Current user profile |
| PUT | `/api/auth/me` | Update profile (name, password) |
| GET | `/api/users` | List users (admin) |
| PUT | `/api/users/{id}/role` | Update user role (admin) |
| PUT | `/api/users/{id}/password` | Reset user password (admin) |
| DELETE | `/api/users/{id}` | Delete user (admin) |
| GET | `/api/invite-codes` | List invite codes (admin) |
| POST | `/api/invite-codes` | Create invite code (admin) |
| DELETE | `/api/invite-codes/{code}` | Delete invite code (admin) |
| GET | `/api/health` | Health check |
| GET | `/api/status` | System status (uptime, memory, component counts) |
| POST | `/api/chat` | Send a chat message (HTTP, non-streaming) |
| POST | `/api/chat/message` | Send a message with file attachment (multipart form) |
| GET | `/api/sessions` | List sessions |
| GET | `/api/sessions/{key}` | Get session with messages |
| PUT | `/api/sessions/{key}/activate` | Set session as active for its channel |
| DELETE | `/api/sessions/{key}` | Delete session |
| GET | `/api/tasks` | List tasks |
| POST | `/api/tasks` | Create task |
| GET | `/api/tasks/{id}` | Get task |
| PUT | `/api/tasks/{id}` | Update task |
| DELETE | `/api/tasks/{id}` | Delete task |
| POST | `/api/tasks/{id}/trigger` | Manually trigger task (202 Accepted) |
| GET | `/api/tasks/{id}/runs` | List runs for task |
| GET | `/api/runs` | List runs (filterable by status, task_id) |
| GET | `/api/runs/recent` | Recent runs |
| GET | `/api/runs/{id}` | Get run details (includes tool calls) |
| POST | `/api/runs/{id}/cancel` | Cancel a running task |
| GET | `/api/memory/stats` | Memory statistics |
| GET | `/api/memory/nodes` | List memory nodes |
| POST | `/api/memory/nodes` | Create memory node |
| GET | `/api/memory/nodes/{id}` | Get memory node |
| DELETE | `/api/memory/nodes/{id}` | Delete memory node |
| PATCH | `/api/memory/nodes/{id}/pin` | Pin or unpin memory node |
| GET | `/api/memory/nodes/{id}/edges` | Get edges for node |
| GET | `/api/memory/nodes/{id}/connected` | Get connected nodes |
| GET | `/api/skills` | List installed skills |
| GET | `/api/skills/search` | Search ClawHub |
| POST | `/api/skills/install` | Install skill |
| DELETE | `/api/skills/{id}` | Uninstall skill |
| GET | `/api/tools` | List registered tools |
| GET | `/api/tools/{name}` | Get tool definition |
| DELETE | `/api/tools/{name}` | Delete custom tool |
| GET | `/api/notifications` | List notifications (paginated, unread count) |
| PUT | `/api/notifications/{id}/read` | Mark notification as read |
| PUT | `/api/notifications/read-all` | Mark all notifications as read |
| DELETE | `/api/notifications/{id}` | Delete a notification |
| DELETE | `/api/notifications` | Delete all notifications |
| POST | `/api/push-tokens` | Register Expo push token |
| DELETE | `/api/push-tokens` | Unregister all push tokens (logout) |
| GET | `/api/settings` | Get settings |
| PUT | `/api/settings` | Update settings |
| GET | `/api/version` | Version info and update status |
| POST | `/api/version/check` | Trigger update check |
| POST | `/api/version/download` | Download latest release |
| POST | `/api/version/restart` | Apply downloaded update and restart |
| POST | `/api/version/skip` | Skip a specific version |
| GET | `/api/connectors` | List connectors |
| GET | `/api/connectors/{name}/status` | Connector connection status |
| GET | `/api/connectors/{name}/auth/start` | Start OAuth flow |
| DELETE | `/api/connectors/{name}/auth` | Disconnect connector |
| GET | `/api/connectors/{name}/settings` | Get connector settings (calendars) |
| PUT | `/api/connectors/{name}/settings` | Update enabled calendars |
| POST | `/api/connectors/{name}/settings/refresh` | Refresh calendar list from provider |
| GET | `/api/mcp/servers` | List MCP servers |
| POST | `/api/mcp/servers` | Add MCP server |
| PATCH | `/api/mcp/servers/{name}` | Update server instructions |
| DELETE | `/api/mcp/servers/{name}` | Remove MCP server |
| POST | `/api/mcp/servers/{name}/start` | Start server |
| POST | `/api/mcp/servers/{name}/stop` | Stop server |
| PUT | `/api/mcp/servers/{name}/secrets` | Update server secrets |
| GET | `/api/mcp/servers/{name}/tools` | List server tools |
| POST | `/api/mcp/servers/{name}/tools/{tool}/test` | Test a tool call |

## Workspace Layout

Cogitator stores all persistent data in a workspace directory (default: `./data`).

```
data/
  cogitator.db          SQLite database (sessions, messages, tasks, runs, memory nodes, edges)
  cogitator.yaml        Persisted config (no secrets)
  secrets.yaml          Legacy secrets file (migrated to OS keychain on first run)
  mcp.json              MCP server configurations
  content/
    profile.md          Agent personality/system prompt
    memories/           Content files for memory nodes
    transcripts/        Conversation transcripts
  skills/
    installed/          Downloaded skill packages
    learned/            Skills the agent has learned from experience
  tools/custom/         Custom tool YAML definitions
  sandbox/              Working directory for shell commands (isolated from config/secrets)
```

## License

Private.

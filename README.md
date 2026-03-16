# Cogitator

**[cogitator.cloud](https://cogitator.cloud)**

A personal AI agent that remembers what you tell it, works while you sleep, and gets smarter the longer you use it.

Most AI assistants remember isolated facts about you. Cogitator builds a knowledge graph: facts connect to preferences, preferences connect to patterns, and the agent adapts its behavior based on evidence over time. It runs tasks autonomously on schedules you define, fixes its own failures, and gets measurably better the longer you use it.

One Go binary. One SQLite database. No Redis, no Postgres, no Kubernetes. Runs on your Mac or a $5/month VPS.


## What makes it different

**Memory that connects.** A persistent knowledge graph with vector retrieval. Facts link to preferences, preferences link to patterns, and relationships between ideas surface automatically. The agent on day 100 is measurably different from day 1.

**Behavioral adaptation.** A compact profile that evolves based on evidence. Three feedback loops at different timescales: micro-reflection during conversations, immediate debugging after task failures, and periodic revision that synthesizes new rules from accumulated episodes.

**Autonomous tasks.** Schedule tasks that run on cron with the full agent loop. Morning briefings, weekly reports, website monitoring, notifications. When a task fails, the agent reads the failure log, figures out what went wrong, and fixes it.

**Two-tier model economics.** Every operation is classified as cheap or standard. Memory enrichment, retrieval, and routine tasks run on inexpensive models. Complex reasoning and conversations use capable models. This cuts costs 60 to 80% compared to running everything on a frontier model.

**Built-in observability.** Token consumption tracked per model tier, per day, with visual charts. Every task run audited with tool calls, duration, error classification, and full transcripts.

**Multi-user.** Multiple people share one instance. Each person gets their own sessions, tasks, and private memories. Shared memories let the whole household or team benefit from common knowledge. Invite codes for onboarding.


## Quick start

```sh
git clone https://github.com/cogitatorai/cogitator.git
cd cogitator

# Install dashboard dependencies and build
cd dashboard && npm ci && npm run build && cd ..

# Build the server
cd server && go build -trimpath -o ../cogitator ./cmd/cogitator && cd ..

# Copy the .env file (recommended to set the JWT param to enable persistent sessions)
cp .env.example .env

# Run
./cogitator
```

Open `http://localhost:8484`. Create your admin account, configure an LLM provider in Models, and start a conversation.

### Requirements

- Go 1.25+
- Node.js 20+ (for dashboard build)
- An OpenAI-compatible LLM provider (OpenAI, Anthropic, Groq, Together, OpenRouter, or local Ollama)


## Development

```sh
# Install dashboard dependencies
cd dashboard && npm ci && cd ..

# Start backend + dashboard dev server (hot reload)
# From project root:
cd server && go build -trimpath -o ../../cogitator ./cmd/cogitator && cd .. && \
  trap 'kill 0' EXIT && ./cogitator & cd dashboard && npm run dev

# Run Go tests
cd server && go test ./... -count=1

# Lint
cd server && go vet ./...
```

If you are building from the parent monorepo (which includes platform wrappers, mobile app, and other components), a top-level Makefile provides convenience targets: `make all`, `make dev`, `make test`, `make lint`.


## Docker

Pre-built images for amd64 and arm64 are published to [Docker Hub](https://hub.docker.com/r/deiu/cogitator) on every release:

```sh
docker pull deiu/cogitator
```

To build from source and run with Compose:

```sh
docker compose up
```

The container exposes port 8484 by default and persists state to a `/data` volume (the host path is configurable via `COGITATOR_WORKSPACE_PATH` in `.env`). Copy `.env.example` to `.env` before starting.


## Architecture

```
server/
  cmd/cogitator/        Entry point
  internal/
    agent/              LLM agent loop with tool calling and context building
    api/                HTTP/REST API and route handlers
    auth/               JWT, refresh tokens, request context
    bus/                In-process event bus
    channel/            Chat transports (WebSocket, Telegram)
    config/             YAML config with environment variable overrides
    database/           SQLite with WAL mode, auto-migrations
    mcp/                MCP server lifecycle, tool discovery, remote transport
    memory/             Knowledge graph: nodes, edges, vector embeddings, retrieval
    provider/           LLM provider abstraction (OpenAI-compatible)
    sandbox/            Shell execution sandbox (host or Docker)
    security/           Path filtering, command blocking, domain allowlisting, audit
    session/            Conversation and message persistence
    skills/             Skill discovery, installation, and management
    task/               Scheduled task execution, runs, cancellation
    tools/              Built-in and custom tool registry
    user/               User store, invite codes, authentication
    worker/             Background workers: enrichment, profiling, consolidation
    workspace/          File system layout for data, memories, skills, tools

dashboard/
  src/                  React 19 + Vite + Tailwind CSS SPA
  public/               Static assets
```

## Features

### Tools

24 built-in tools plus custom tool support via YAML definitions.

| Tool | Description |
|---|---|
| `read_file` | Read workspace files |
| `write_file` | Write workspace files |
| `list_directory` | List directory contents |
| `shell` | Execute shell commands (sandboxed) |
| `create_task` | Create a scheduled task |
| `update_task` | Update an existing task |
| `list_tasks` | List all tasks |
| `run_task` | Manually trigger a task |
| `delete_task` | Delete a task |
| `toggle_task` | Enable or disable a task |
| `heal_task` | Diagnose and fix a task from its last run |
| `search_skills` | Search for skills |
| `install_skill` | Install a skill |
| `create_skill` | Create a custom skill |
| `update_skill` | Update an installed skill |
| `list_installed_skills` | List installed skills |
| `read_skill` | Read skill instructions |
| `save_memory` | Save to long-term memory |
| `list_users` | List household members |
| `toggle_memory_privacy` | Toggle a memory between private and shared |
| `allow_domain` | Add a domain to the network allowlist |
| `fetch_url` | Fetch a web page and convert to markdown |
| `web_search` | Search the web via DuckDuckGo, Brave, or Mojeek |
| `notify_user` | Send a notification to another user |
| `start_mcp_server` | Start a configured MCP server by name |

Custom tools are defined as YAML files in the `tools/` directory (within the defined workspace) with a name, description, parameters schema, and shell command template.

### MCP servers

Connect external tools via the Model Context Protocol. Add local (stdio) or remote (SSE/Streamable HTTP) servers through the dashboard or API. The agent discovers server tools automatically, enriches their descriptions, and generates a skill per server so it knows when and how to use each tool. Remote servers support header-based auth and OAuth. Servers auto-reconnect with exponential backoff.

### Connectors

OAuth-based integrations with external services. Google Calendar and Gmail ship built in. The agent calls calendar tools during conversation to check schedules, list events, and search across multiple calendars. The connector runtime supports custom connectors defined as YAML manifests with OAuth config, REST endpoints, and jq-based response mapping.

**Chrome Browser connector** gives the agent CDP-based browser control: navigate pages, read content via accessibility tree snapshots, click elements, type text, evaluate JavaScript, take screenshots, and more. Requires Chrome 146+ with debugging enabled in `chrome://inspect/#remote-debugging`. Enable on the Connectors page in the dashboard.

### Security

API keys, bot tokens, and OAuth credentials are stored in the OS keychain (macOS Keychain, Windows Credential Manager, Linux secret-service) with file-based fallback. Shell commands run in an isolated subdirectory with a scrubbed environment (variables containing API_KEY, SECRET, TOKEN, PASSWORD, etc. are stripped). Sensitive files are blocked from shell access by path filtering. Optional Docker sandboxing runs commands in throwaway containers with memory/PID limits, read-only rootfs, and no network by default.

### Dashboard

Pages: Chat, Tasks, Memory, Skills, Connectors, MCP Servers, History, Resources, Account, Users (admin), Settings, Models (admin). Client-mode lets you connect the dashboard to a remote Cogitator server by entering its URL and credentials.


## Configuration

Cogitator also uses a `cogitator.yaml` config file with environment variable overrides. The config is auto-saved to the workspace directory so dashboard settings persist across restarts.

The precedence is: env vars > cogitator.yaml > defaults.

```sh
# Environment variables (.env or export)
COGITATOR_SERVER_PORT=8484
COGITATOR_SERVER_HOST=0.0.0.0
COGITATOR_WORKSPACE_PATH=~/.cogitator
COGITATOR_JWT_SECRET=<random-hex-string>

# Model configuration (optional, can also be set in the dashboard)
COGITATOR_MODEL_PROVIDER=openai        # openai, anthropic, ollama, groq, together, openrouter
COGITATOR_MODEL=gpt-4o                 # model identifier for the primary (standard) slot
COGITATOR_MEMORY_EMBEDDING_MODEL=text-embedding-3-small  # embedding model (auto-selects nomic-embed-text for Ollama)

# Connectors and login (optional OAuth credentials)
GOOGLE_CLIENT_ID=<your-google-client-id>
GOOGLE_CLIENT_SECRET=<your-google-client-secret>
```

Provider credentials can be set via environment variables or through the Settings page. Credentials are stored in the OS keychain with automatic fallback to file.

**Model tiers.** Tasks and conversations use a two-tier model system (standard and cheap). Configure each tier with a provider and model name via the Settings page.

**Social sign-in.** To enable "Sign in with Google", set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in `.env`. Create OAuth 2.0 credentials in the [Google Cloud Console](https://console.cloud.google.com/apis/credentials) and add your callback URL (`http://localhost:8484/api/auth/google/callback`) as an authorized redirect URI. The macOS desktop app injects these at build time via ldflags; Docker and CLI builds read them from environment variables.


## Workspace layout

```
~/.cogitator/
  cogitator.db          SQLite database
  cogitator.yaml        Persisted config (no secrets)
  mcp.json              MCP server configurations
  content/
    profile.md          Agent personality / system prompt
    memories/           Content files for memory nodes
    transcripts/        Conversation transcripts
  skills/
    installed/          Downloaded skill packages
    learned/            Skills the agent has learned
  tools/custom/         Custom tool YAML definitions
  sandbox/              Working directory for shell commands
```


## API

All endpoints require JWT authentication. The WebSocket endpoint is at `/ws`.

<details>
<summary>Full API reference</summary>

| Method | Path | Description |
|---|---|---|
| GET | `/api/auth/needs-setup` | Check if first-run setup is required |
| POST | `/api/auth/setup` | Create initial admin account |
| POST | `/api/auth/register` | Register with invite code |
| POST | `/api/auth/login` | Login (returns JWT + refresh token) |
| POST | `/api/auth/refresh` | Refresh access token |
| POST | `/api/auth/logout` | Revoke refresh token |
| POST | `/api/auth/social` | Social sign-in (Google/Apple ID token) |
| GET | `/api/auth/providers` | Available social auth providers |
| GET | `/api/auth/me` | Current user profile |
| PUT | `/api/auth/me` | Update profile |
| GET | `/api/users` | List users (admin) |
| PUT | `/api/users/{id}/role` | Update user role (admin) |
| DELETE | `/api/users/{id}` | Delete user (admin) |
| GET | `/api/invite-codes` | List invite codes (admin) |
| POST | `/api/invite-codes` | Create invite code (admin) |
| DELETE | `/api/invite-codes/{code}` | Delete invite code (admin) |
| GET | `/api/health` | Health check |
| GET | `/api/status` | System status |
| POST | `/api/chat` | Send a chat message |
| POST | `/api/chat/message` | Send message with file attachment |
| GET | `/api/sessions` | List sessions |
| GET | `/api/sessions/{key}` | Get session with messages |
| PUT | `/api/sessions/{key}/activate` | Set session as active |
| DELETE | `/api/sessions/{key}` | Delete session |
| GET | `/api/tasks` | List tasks |
| POST | `/api/tasks` | Create task |
| GET | `/api/tasks/{id}` | Get task |
| PUT | `/api/tasks/{id}` | Update task |
| DELETE | `/api/tasks/{id}` | Delete task |
| POST | `/api/tasks/{id}/trigger` | Manually trigger task |
| GET | `/api/tasks/{id}/runs` | List runs for task |
| GET | `/api/runs` | List runs |
| GET | `/api/runs/recent` | Recent runs |
| GET | `/api/runs/{id}` | Get run details |
| POST | `/api/runs/{id}/cancel` | Cancel a running task |
| GET | `/api/memory/stats` | Memory statistics |
| GET | `/api/memory/nodes` | List memory nodes |
| POST | `/api/memory/nodes` | Create memory node |
| GET | `/api/memory/nodes/{id}` | Get memory node |
| DELETE | `/api/memory/nodes/{id}` | Delete memory node |
| PATCH | `/api/memory/nodes/{id}/pin` | Pin or unpin memory |
| GET | `/api/memory/nodes/{id}/edges` | Get edges for node |
| GET | `/api/memory/nodes/{id}/connected` | Get connected nodes |
| GET | `/api/skills` | List installed skills |
| GET | `/api/skills/search` | Search for skills |
| POST | `/api/skills/install` | Install skill |
| DELETE | `/api/skills/{id}` | Uninstall skill |
| GET | `/api/tools` | List registered tools |
| GET | `/api/tools/{name}` | Get tool definition |
| DELETE | `/api/tools/{name}` | Delete custom tool |
| GET | `/api/notifications` | List notifications |
| PUT | `/api/notifications/{id}/read` | Mark notification as read |
| PUT | `/api/notifications/read-all` | Mark all as read |
| PUT | `/api/notifications/read-tasks` | Mark task notifications as read |
| DELETE | `/api/notifications/{id}` | Delete notification |
| DELETE | `/api/notifications` | Delete all notifications |
| POST | `/api/push-tokens` | Register push token |
| DELETE | `/api/push-tokens` | Unregister push tokens |
| GET | `/api/settings` | Get settings |
| PUT | `/api/settings` | Update settings |
| GET | `/api/version` | Version info |
| GET | `/api/connectors` | List connectors |
| GET | `/api/connectors/{name}/status` | Connector status |
| GET | `/api/connectors/{name}/auth/start` | Start OAuth flow |
| DELETE | `/api/connectors/{name}/auth` | Disconnect connector |
| GET | `/api/connectors/{name}/settings` | Connector settings |
| PUT | `/api/connectors/{name}/settings` | Update connector settings |
| GET | `/api/connectors/browser/status` | Browser connector status |
| POST | `/api/connectors/browser/enable` | Enable browser connector |
| POST | `/api/connectors/browser/disable` | Disable browser connector |
| GET | `/api/mcp/servers` | List MCP servers |
| POST | `/api/mcp/servers` | Add MCP server |
| PATCH | `/api/mcp/servers/{name}` | Update server |
| DELETE | `/api/mcp/servers/{name}` | Remove MCP server |
| POST | `/api/mcp/servers/{name}/start` | Start server |
| POST | `/api/mcp/servers/{name}/stop` | Stop server |
| GET | `/api/mcp/servers/{name}/tools` | List server tools |

</details>


## Platforms

**macOS app.** A native .app bundle wraps the server with a WebKit-based UI, system notifications, and auto-updates from GitHub releases. Available at [cogitator.cloud](https://cogitator.cloud).

**iOS and Android.** A React Native companion app connects to a running Cogitator instance.

**Docker.** Single container, single volume. See the Docker section above.

**Bare metal.** The Go binary runs anywhere Go compiles. Point it at a workspace directory and go.


## Contributing

Contributions are welcome. Bug fixes, new channel adapters, connectors, dashboard improvements, and test coverage can go straight to a pull request. For anything that touches the agent loop, memory system, or security model, open a discussion first.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guidelines.


## License

AGPL-3.0. See [LICENSE.md](LICENSE.md).

The server and dashboard source code are free to use, modify, and distribute under the terms of the GNU Affero General Public License v3.0. If you run a modified version as a network service, you must make your source code available to users of that service.

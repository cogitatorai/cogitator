# Changelog

All notable changes to this project will be documented in this file.

## 0.44.1 (2026-03-27)

### Features

- Added GPT-5.4 to OpenAI and OpenRouter model selection lists (3a03945)

### Improvements

- Docker Hub overview now syncs automatically from README on releases and README changes (85b8a9f)
- Fixed project URL to cogitator.me in README (f255ecb)

## 0.44.0 (2026-03-27)

### Features

- Task toggles (enabled, notify chat, broadcast) now update optimistically with rollback on failure (f622e03)

### Improvements

- Replaced "Cogitator" label with "Agent" on assistant chat messages (f622e03)
- Cleaned up Resources page header layout (3f72521)

## 0.43.1 (2026-03-26)

### Fixes

- Tasks that use `notify_user` no longer produce duplicate notifications; the recipient gets only the direct message, and the task owner is no longer bothered with a redundant "task completed" alert (0f0d36b)

## 0.43.0 (2026-03-25)

### Features

- New `list_available_tools` built-in tool lets the agent inspect all registered capabilities (built-in, connector, MCP) before searching ClawHub, preventing duplicate skill recommendations (7e4b081)
- MCP server instructions now rendered in the system prompt so the agent knows what stopped servers provide and can start them instead of installing redundant skills (0777941)

### Fixes

- `UpdateNode` now persists type field changes; enricher type reclassifications were silently lost on save
- Memory list visibility: owners always see their own nodes regardless of subject_id filtering
- Admin users bypass visibility filtering on memory node list and graph endpoints
- Enricher preserves original type for skill and task_knowledge nodes instead of reclassifying them
- `NodeSkill` added as valid enrichment type, preventing skills from being reclassified to "fact"
- Migration re-enriches nodes with stuck types and restores incorrectly reclassified skill nodes
- Favicon regenerated with light background for better visibility

## 0.42.2 (2026-03-24)

### Fixes

- WebSocket connections no longer drop through reverse proxies; server now pings clients every 30 seconds to keep connections alive and detect dead peers

## 0.42.1 (2026-03-24)

### Fixes

- Skill editing now self-heals when SKILL.md files are missing from disk, e.g. when a database is copied to a new Docker volume without the skill files (recreates parent directory before writing)
- Embedded Go timezone database (`time/tzdata`) so scheduled tasks respect the `TZ` environment variable in minimal Docker containers (Alpine)
- Skill content read API returns 404 with actionable message when the skill file is missing instead of a generic 500 error
- Dashboard skill editor surfaces load and save errors to the user instead of failing silently

### Improvements

- Documented Google connector tools (calendar_list, calendar_search, email_search, email_read) in README

## 0.42.0 (2026-03-23)

### Features

- Store task output metadata (task name, status, trigger, IDs) as structured JSON in a new `metadata` column instead of baking it into markdown content (1b3f04d)
- Task system prompt now instructs the LLM to output only the result, keeping metadata separate for richer client-side rendering

## 0.41.2 (2026-03-23)

### Fixes

- Use public_url config for OAuth callback URLs in connector and social login flows, fixing Google OAuth in Docker and remote deployments (be39c77)
- Hot-reload public_url when settings are updated (no restart needed) (be39c77)

## 0.41.1 (2026-03-23)

### Fixes

- Cron schedule description and picker misclassified complex expressions (ranges, lists) as "yearly" or "monthly" instead of "custom schedule" (acc5130)
- Replace "cron" with "automated" in user-facing trigger labels in task output headers and history (5605298)
- Upgrade jsonparser to 1.1.2 to fix GHSA-6g7g-w4f8-9c9x out-of-bounds read vulnerability (c7b2700)

## 0.41.0 (2026-03-23)

### Features

- Surface expired OAuth credentials in connector status: red badge, error message, and reconnect button across dashboard and agent system prompt (871334e)
- Show connector display name and provider icon on OAuth callback success page (62d7805)

### Fixes

- Handle categorized retrieval_triggers from LLMs that return objects instead of arrays (cc5c8d0)
- Sanitize OAuth token errors in API responses to avoid leaking token endpoint details (871334e)
- HTML-escape heading and detail in OAuth branded callback page (62d7805)

## 0.40.0 (2026-03-20)

### Features

- Memory system hardening: server-side validation for enrichment output, cosine-weighted graph edges, and node deduplication via embedding similarity + title Jaccard (35fd121..e4d67ae)
- Programmatic consolidation replaces LLM synthesis with tag-frequency pattern construction (88d34c5)
- Programmatic profiler replaces LLM revision with structured template from graph queries (b0a588e)
- Pattern-based behavioral signal detection for English conversations, LLM fallback for non-English (ee20433, d6bb917)
- Confidence-based retrieval scoring: formula is now `similarity * confidence * type_boost` with evidence-based adjustments from contradictions, corrections, and acknowledgments (01675e1, b490683)
- Simplified save_memory tool: removed node_type and retrieval_triggers params, auto-title from content (382999e, 7d992ea)
- Node ownership and visibility redesign: `private` boolean flag replaces NULL-based user_id visibility, edge privacy cascades from connected nodes (aecbdb6..49c8e04)
- Token-budget retrieval with configurable similarity threshold, type boost, and type filtering (b6412db, e85f22d)
- Enrichment version tracking with automatic re-enrichment when version is bumped (ac77aac, c404b7c)
- Embedded migrations: numbered SQL migrations are now embedded in the binary and run automatically on startup (19d49c3)

### Fixes

- Person name sanitization uses prefix-only replacement to preserve names as values in titles and summaries (fe142bf, 4dd4ab5)
- Enricher no longer attributes memories to the wrong user for shared nodes (2afd7b9, d8623f8)
- Migration runner handles duplicate column errors gracefully for idempotent upgrades (19d49c3)
- Memory preamble instructs agent to actively use retrieved memories and avoid redundant questions (4fbd490)

### Improvements

- Removed recencyBoost and ConfidenceDecayDays: retrieval ranks by semantic relevance and confidence, not time (b90bab1, 4954e7c)
- Enrichment prompt requests up to 100 cross-domain retrieval triggers across direct, contextual, and lateral categories (a2a29a6)
- Node content included in embedding text for richer vector representations (ea18687)
- Exported worker types (BuildEnrichmentPrompt, BuildReflectionPrompt, DetectSignals) for external tooling (ebd88b3)

## 0.39.3 (2026-03-17)

### Improvements

- README updated with voice feature documentation, architecture, and mobile voice support (e6e5512)

## 0.39.2 (2026-03-17)

### Fixes

- Voice brevity hint no longer visible in chat history; added MessageSuffix field to ChatRequest that is sent to the LLM but excluded from stored messages (8e1b738)
- Test coverage for MessageSuffix storage/LLM split (fba267e)

## 0.39.1 (2026-03-16)

### Improvements

- Voice responses are concise: LLM instructed to reply in 1-2 sentences without markdown when responding to voice input (5983b40)
- Connector results (calendar events, emails) summarized concisely instead of dumping raw fields (4c31336)

## 0.39.0 (2026-03-16)

### Features

- Voice conversation mode with bidirectional speech-to-text and text-to-speech (186329f..c75c4a4)
- Provider-agnostic voice interfaces with OpenAI Whisper (STT) and OpenAI TTS as first implementations (186329f, a1edc15, 4e9c650)
- Voice settings in the dashboard Models page: STT/TTS provider selection and voice picker (9598fc1, 7cf7adb)
- Hot-reload voice providers when settings change without server restart (13c48c1)
- Streaming TTS audio chunks from provider to client in real-time (52e1ace)
- `POST /api/chat/voice` endpoint with async LLM + TTS processing (c75c4a4)
- Voice event types on the event bus with high-throughput subscriber buffering (ff47812, ca15971)

### Fixes

- Preserve user display name when resetting password via admin panel (c8ecce7)
- Sanitize voice error messages sent to clients to prevent API key leakage (86ca7b7)

### Improvements

- Rename Admin page to Models in the dashboard navigation (49f66c9, 8dcda42, f79585c)
- Models page icon changed from Shield to Cpu (83ee423)
- Models page shown before Users in the navigation menu (8a04a69)

## 0.38.0 (2026-03-15)

Closes [#2](https://github.com/cogitatorai/cogitator/issues/2).

### Features

- Configurable embedding model via environment variable (`COGITATOR_MEMORY_EMBEDDING_MODEL`), settings API, and dashboard UI (7988a3c, a388b87, b0411e4)
- Auto-selects `nomic-embed-text` when Ollama is the provider and no embedding model is explicitly set (7988a3c)
- Per-provider embedding model dropdown with curated options for OpenAI, Ollama, Together, and OpenRouter (ee9008f, debd13d)
- When no Ollama embedding models are pulled locally, suggests `nomic-embed-text` and `mxbai-embed-large` (115892f)
- Auto-selects embedding model after pulling a known embedding model on Ollama (e85b88e)
- Auto-clears embedding model selection when its Ollama model is deleted (36b8c46)
- Providers without embedding support (Anthropic, Groq) show a clear informational message (1a5b107)
- MiniMax M2.5 added to OpenRouter model list (ee9008f)

### Fixes

- Ollama model pull retries on expired JWT token instead of failing with "invalid or expired token" (017a94b)
- Rapid embedding model changes no longer race; in-flight re-embed is cancelled before starting a new one (cfa12cd)
- All model dropdowns (primary, secondary, embedding) refresh immediately after Ollama pull or delete (c531fd9, d568b34)
- Background re-embed errors are now logged instead of silently discarded (a04d272)
- Ollama embedding model matching handles `:latest` tag suffix (dbc8790)

## 0.37.1 (2026-03-15)

### Fixes

- Settings page hides admin-only sections (Workspace, Telegram, Security) for non-admin users (57f6ca5)
- Resources page restricted to admin users only (57f6ca5)
- MCP server management controls (Add, Start, Stop, Remove, Test) hidden for non-admin users (57f6ca5)
- Browser connector uses desktop-mode environment check instead of admin role; any user on the local macOS app can enable/disable Chrome (57f6ca5)
- Browser connector shows contextual unavailability message for web app and client mode (57f6ca5)

## 0.37.0 (2026-03-15)

### Features

- Chrome Browser connector: CDP-based browser control with 15 tools (navigate, snapshot, click, type, eval, scroll, screenshot, and more) (a2f7d29)
- Auto-discovery via Chrome's DevToolsActivePort file; requires Chrome 146+ with debugging enabled in chrome://inspect (23aeddc, 6d1cc3f)
- Browser connector auto-reconnects on server startup when previously enabled (57c4d64)
- Chrome Browser card on the Connectors dashboard page with enable/disable toggle (a2f7d29)
- Shared ConnectorCard component with stable height, truncated titles, and expandable descriptions (0ea8d10, f2c4c62)

## 0.36.0 (2026-03-13)

### Features

- Cross-user notifications: tasks can target specific users via notify_users field (93b703e)
- notify_user built-in tool allows agents to send messages to other users (db0cfd1)
- Per-user tasks:output sessions so each user sees only their own task results (462a0b7)
- Targeted WebSocket delivery for selective task notifications (497a5f4)
- Agent system prompt describes notification capabilities (753fb00)

### Fixes

- Task notifications now clear when navigating to the Tasks message list (3e5f2bf)
- Task list scoped to calling user; notifications surfaced in tasks list (b76ff55)
- Makefile git describe resolves to correct directory (42260f4)

## 0.35.0 (2026-03-13)

### Features

- Admin task view: owner column with filter dropdown to see and filter tasks by user (013c55c)
- Broadcast notifications: tasks with the broadcast flag now notify all registered users (e8355d5)

### Fixes

- User-friendly error messages replace raw HTTP status codes and "Failed to fetch" throughout the dashboard (1378fe7)
- Task execution prompt clarifies that the agent's text response is delivered as the notification (6757b1a)
- Button height alignment in server settings panel (2e139fe)

## 0.34.0 (2026-03-13)

### Features

- Dashboard client-mode: connect to a remote Cogitator server from the web UI by entering a server URL and credentials (8e12db7)

### Fixes

- WebSocket status updates, responses, and errors are now scoped to the originating chat session; switching sessions no longer causes spinners or messages from another session to bleed into the current view (781dcf2)

## 0.33.0 (2026-03-12)

### Features

- Chat input redesigned as auto-resizing composer with inline attach and send buttons; chat bubbles widened to 80% (d4684ed)

### Improvements

- Docker image published to Docker Hub automatically on release (6571489)
- Removed legacy orchestrator code from public repo (1f4bd49)

## 0.32.0 (2026-03-12)

### Features

- macOS app minimizes to the status bar when closed via the window button or Cmd+W; the server keeps running in the background and the window can be restored from the status bar menu; Cmd+Q still fully quits

### Fixes

- Agent file read/write tools now block access to cogitator.db and mcp.json as sensitive paths
- IsSensitivePath now matches bare filename patterns (cogitator.yaml, secrets.yaml, cogitator.db, mcp.json) by basename, fixing a bug where these were only enforced for shell commands but not for read_file/write_file

## 0.31.2 (2026-03-12)

### Fixes

- Remove VOLUME directive from Dockerfile that caused stale anonymous volumes to shadow bind mounts (f1660ce)
- Vite dev proxy reads server port from .env instead of hardcoding 8484 (f1660ce)
- Remove stale embedded dashboard dist files checked into the repo (ddfa238)

## 0.31.1 (2026-03-12)

### Features

- Branded Cogitator callback pages for OAuth flows in the desktop app (4c58843)
- Web OAuth flows redirect back to the dashboard instead of showing a close-me page (4c58843)
- Social sign-in credentials fall back to GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET env vars for Docker (4c58843)

### Fixes

- OAuth redirect_uri mismatch when a dev proxy sits between the browser and server (4c58843)
- Desktop app no longer loads a full dashboard in the system browser after OAuth (4c58843)

## 0.31.0 (2026-03-12)

### Features

- Reflector worker classifies behavioral signals from conversations and feeds the enrichment pipeline (7925523)
- Update banner uses proper semver comparison; Docker/CLI installs show "View Release" instead of "Update Now" (fe6c88f)
- Version displayed on login, register, and signup pages; version endpoint is now public (43c79a3)

### Fixes

- Delete task_runs and token_usage when removing a user, preventing FK constraint 500 errors (cc27a44)
- Docker image now stamps the version via ldflags at build time (83d8819)
- Docker Compose workspace path uses HOME instead of tilde and sets COGITATOR_WORKSPACE_PATH inside the container (83d8819)
- Warm color palette and improved font readability across light and dark themes (80b7289)

## 0.30.0 (2026-03-12)

### Features

- Multi-stage Docker build bundles the dashboard into the image (29ee25c)
- Auto-detect dashboard dist directory in CLI mode relative to the binary (cf147e7)
- Docker Compose configuration with env var support for port and workspace path (29ee25c)

### Fixes

- Parse base64-encoded invite codes in all registration endpoints (da00136)
- LLM provider banner only shown to admin users (4281543)
- LLM provider banner navigates to Admin instead of Settings (3e8675f)
- Swift wrapper binary moved to bin/ to avoid macOS case-insensitive collision (25e48df)
- Resources page padding now matches other dashboard pages (f2ecb28)

### Improvements

- Server and dashboard moved into cogitator/ directory for public repo distribution (ce1887a)
- Go build outputs consolidated into bin/ directory (d4e218d)

## 0.29.0 (2026-03-11)

### Features

- Load environment variables from `.env` file with shell env fallback (7533939)
- Expand tilde (`~`) in workspace paths for `.env` compatibility (7533939)
- Auto-cleanup build artifacts after release builds (9d64a3e)

### Fixes

- Web app session now persists across page refreshes; deduplicate concurrent refresh token calls triggered by React StrictMode (7533939)
- Browser notification toggle awaits permission result and shows a message when blocked (7533939)
- Browser notifications prefixed with "Cogitator:" for clarity outside the app (7533939)
- Vite WS proxy error filter catches all proxy-related messages (7533939)
- Improved agent instructions for file attachments and honesty (e415f78)

## 0.28.0 (2026-03-10)

### Features

- Session title generated immediately from user prompt instead of after agent response (65c5b60)
- Highlighted "get to know each other" suggestion on desktop and mobile, strong when no memories exist (f385392)
- Homepage updates with SEO assets and "made with" footer (8d33772)

### Fixes

- Desktop notifications suppressed only when window is active and on the target page (597e717)
- Active chat session persists when navigating away and back (468b52c)
- Mobile tools used badge now expandable to show skill, tool, and memory names (a997ce8)
- Mobile session list refreshes when app returns to foreground (6c02b51)

## 0.27.0 (2026-03-10)

### Features

- Redesigned homepage with updated layout, animations, and SVG assets (31a1c15)
- Beta label in dashboard sidebar (ece0118)

## 0.26.1 (2026-03-10)

### Fixes

- Update banner now appears within a minute of detection instead of up to 30 minutes (065e0f7)

## 0.26.0 (2026-03-10)

### Features

- Public release distribution via `cogitatorai/cogitator` GitHub repo (c347fa2)
- Auto-updater now fetches from public repo; no GitHub token required (c347fa2)

### Improvements

- Consolidated database migrations (V1-V8) into a single unified schema (c347fa2)
- Homepage animation and styling refresh (c347fa2)
- Removed stale dashboard dist files from source tree (c347fa2)

## 0.25.0 (2026-03-10)

### Features

- Bundled "Introduction" skill for onboarding new users with personalized memory graph building
- `update_task` tool: modify task prompt, schedule, model tier, or notification setting in place without deleting and recreating
- Parallel enrichment worker pool (3 concurrent goroutines) with 500ms debounce for rapid event bursts
- Immediate enrichment on memory creation via `EnrichmentQueued` events instead of waiting for 5-minute ticker
- Enrichment status indicator in Memory UI: shows "Processing..." while enrichment is active
- Memory injection guardrails: non-admin users cannot save memories targeting other users

### Fixes

- Frontend update checker now polls every 30 minutes to match backend check interval (was only checking on app startup)
- Self-heal agent no longer silently downgrades task model tier; uses new `update_task` instead of delete/recreate
- Memory retrieval no longer leaks preferences across users; `subject_id` explicitly tracks who each memory is about
- Removed false "about X" attribution for memories without an explicit subject

### Improvements

- `save_memory` enforces atomic memories: enumerated items (e.g. "I like biking, hiking, snorkeling") produce separate memory nodes
- Introduction skill supports multilingual triggering and responds in the user's language
- New "Let's get to know each other" suggestion pill in chat
- Memory stats cards reorganized: Total Nodes, Total Relations, Pending Enrichment
- Hidden Episodes and Task Knowledge tabs from Memory UI (internal node types)
- Enricher drains pending nodes immediately on startup to recover from crashes

## 0.24.0 (2026-03-10)

### Features

- First-run sign-up flow: admin account creation page with social login and email/password when no users exist (a6abc35, 57d3ccd, 731dfd2, 511b0c9, 1cb6e3f)
- Port fallback: server tries configured port, falls back to OS-assigned port and persists it to config for subsequent launches (85101b2)

### Fixes

- Kill orphaned cogitator-server process (scoped to current user) before starting a new instance (3804b16)
- Lazily create tasks:output session on first access so the task panel always appears for new accounts (72dcd28)
- Light theme missing zinc-200 variable causing invisible text in connector settings modal (fb65a0f)

## 0.23.0 (2026-03-09)

### Features

- `fetch_url` tool: fetch web pages and convert HTML to readable markdown, with optional CSS selector extraction and SSRF protection (8e983d2)
- `web_search` tool: search the web by scraping DuckDuckGo, Brave, and Mojeek with automatic rotation and fallback, no API keys required (76d5b69)

### Improvements

- Agent system prompt updated with web fetching and search capabilities (d35505d, 49fb8d0)

## 0.22.0 (2026-03-09)

### Features

- Memory subject tracking: `subject_id` field links memories to the person they describe, surviving name changes and preventing cross-user confusion
- Memory detail UI shows "About" and "Owner" fields with resolved user names
- Memory list filters out memories about other users from the current user's view
- `list_users` tool returns user IDs for stable memory attribution
- `save_memory` tool accepts `subject_id` parameter for explicit person linking
- `tools_used` indicator now appears on file-attachment chat messages (HTTP path)
- Mobile: tools/skills badge on assistant messages
- Mobile: splash screen updated with new swirl logo for light and dark themes
- Dashboard: skill management with create, edit, and search

### Fixes

- Agent no longer confuses the current user with other people mentioned in conversation history or retrieved memories
- Strengthened system prompt with explicit memory attribution rules and repeated user identity

## 0.21.0 (2026-03-09)

### Features

- Dashboard: unified account management merging profile and password editing into a single form with current password verification and email editing

## 0.20.0 (2026-03-09)

### Features

- Desktop onboarding: "existing account" flow for joining a server without an invite code (bac434e)
- Signed and notarized DMG pipeline for macOS distribution with Gatekeeper verification (08fc357, ad9eb13, 646cf5d, b309046)
- New app icon across macOS, mobile, and dashboard favicon (e271708)
- Agent response cancellation with Stop button in dashboard and mobile (f1746c2, 15c8e2d, 3e84d43)
- Mobile: local xcodebuild pipeline replacing EAS Build for TestFlight (8a55107, e45a28e)
- Mobile: edit account screen with password and email updates (37afef0, 023a4b9)
- Mobile: all-chats page with drawer limited to 10 recent sessions (9af8fd6)

### Fixes

- Desktop onboarding crash caused by NSWindow use-after-free on close (44c1e57, d0900c6)
- Desktop onboarding input fields now editable with proper padding and Enter key support (bb2076f, 0bd3470, d0900c6)
- Desktop onboarding uses SF Symbols instead of emoji icons (990ac53)
- Desktop onboarding route corrected from #invite to #register (8ca8cb2)
- Mobile: swipe-to-delete flash and drawer gesture conflict on session rows (80da8ab, f684a9d, c692de8, 94eb6f6)
- Mobile icon alpha channel stripped to fix App Store Connect marketing icon (e271708)
- Dashboard Apple Sign-In buttons hidden until desktop app support is complete (00f34c9)

## 0.19.0 (2026-03-08)

### Features

- Household awareness: cross-user memory sharing with privacy controls and household context injection (29f8ec5)
- Admin-only route guards, per-user budget enforcement, and Users management page (f662800)
- Mobile connectors screen with OAuth flow, calendar settings modal, and deep link redirects (927b524, b34e8cd)
- Mobile drawer restructured with top-level nav items (Chats, Connectors, Notifications, Settings) and session list below (a1ca215)
- Optimistic "Connecting..."/"Disconnecting..." status on connector cards for both mobile and dashboard (966424b)
- Desktop log rotation to prevent unbounded app.log growth (5a1efa2)

### Fixes

- Dashboard polling skips re-render when polled data is unchanged, preventing memory graph flicker (cbfaa57)
- Dashboard connector cards use opaque background so dot grid no longer bleeds through (74fb5e1)
- Mobile connector cards use theme-aware colors for correct light mode styling (ca215db)
- Connector OAuth callback URL derived from request headers for mobile clients while keeping localhost default for desktop (1f8c2fa)
- OAuth callback page shows clean success message instead of attempting auto-close (01b84a8)

## 0.18.0 (2026-03-08)

### Features

- Dashboard theme switcher now supports "System" option that auto-detects OS light/dark preference (ef9f2df)
- Simplified dashboard CSS theme selectors from data-theme attribute to plain class names (ef9f2df)

## 0.17.1 (2026-03-08)

### Fixes

- Redesigned mobile chat UI with full-width assistant messages, right-aligned user bubbles, and card-style input area (6f53cde)
- Tasks sidebar entry now visually distinct with bold uppercase text, left border accent, and increased height on both platforms (6f53cde)
- Task notifications now cleared when opening the Tasks chat page on desktop and mobile (6f53cde)
- Divider lines between consecutive assistant messages for better readability in Tasks chat (6f53cde)
- Removed always-visible connection status bar and Clear History button from desktop chat (6f53cde)

## 0.17.0 (2026-03-08)

### Features

- Email-based accounts replacing username system across backend, dashboard, and mobile (c293c3b, 6b0a9a1, 57258fc, f124316)
- Native Apple Sign-In bridge for macOS desktop app via ASAuthorizationController (766baae, ab1f49f, 44a374c, 8c69a02, ab32c1b)
- Configurable public server URL in admin settings for mobile invite codes (e24c9c7, 6f3ffa1)
- Multiple Apple audience values for mobile and Expo Go sign-in (5c94a36)
- Dev-app Makefile target for debug builds with Expo Go audience (3d1600a)
- Mobile onboarding restructured to prioritize social login with separate email signup screen (f124316)
- Android build job added to mobile CI workflow (d61bb96)

### Fixes

- Apple full name now passed from mobile native SDK to server during social signup (bec3f66)
- Public URL validation requires scheme and host (d38e7c3)
- Admin panel publicUrl state lifted to prevent stale invite code display (e89c224)

## 0.16.0 (2026-03-07)

### Features

- Desktop app respects `server.host` from `cogitator.yaml` instead of forcing `127.0.0.1`
- Mobile app server URL is now editable in Settings, allowing direct connection changes without re-registering
- Social login buttons on mobile now display Google and Apple icons

### Removed

- Relay tunnel system: relay client, protocol, server binary (`cmd/cogitator-relay`), and all relay UI from dashboard Settings
- `BindHost` override in desktop mode; the server now always uses the configured host

## 0.15.0 (2026-03-07)

### Features

- Social sign-in with Google and Apple across web dashboard, macOS desktop, and iOS mobile (44a8ed9, 4f38b78, 3234662, cc93894)
- Connected accounts section on the dashboard Account page for linking/unlinking social providers (4f38b78)
- `/api/auth/providers` endpoint for dynamic social button visibility based on server config (f7c62fc)
- Relay-routed OAuth callback (`/oauth/callback`) enabling a single redirect URI for all relay-tunnelled instances (a6058ac)

### Fixes

- Claim-based token handoff for Google OAuth across separate browser contexts (WKWebView, mobile in-app browser) (6787a52)
- URL-encoded OAuth redirect URIs to support non-standard ports behind relay (a6058ac)

### Tests

- Integration tests for social auth endpoints and account linking (c5cc013, 1a97837)

## 0.14.0 (2026-03-06)

### Features

- OS keychain integration for secret storage via go-keyring: macOS Keychain, Windows Credential Manager, Linux secret-service (ce92887)
- Automatic migration of existing secrets from YAML files to keychain on first startup with idempotent `.bak` rename (ce92887)
- Graceful fallback to file-based storage when keychain is unavailable, determined by a one-time canary probe at startup (ce92887)

### Improvements

- Secrets (OAuth tokens, API keys, MCP credentials) now encrypted by the OS instead of stored as plaintext YAML
- Individual token persistence: connector OAuth saves only the changed token instead of serializing the entire map on every refresh
- Unified `SecretStore` interface used across config, connectors, MCP, and API layers

## 0.13.0 (2026-03-06)

### Features

- Notifications rethought: full page replaced with compact bell dropdown in sidebar header (1da4642, c9d437b)
- Pinned "Tasks" chat session showing task output results with dedicated empty state (c495e78, 1209de4)
- Unread dot indicators on chat sessions with new messages (637f5b2)
- Schedule picker for task configuration with cron expression support (44ffa03)
- `toggle_task` tool lets the agent enable or disable tasks via chat (5175a22)
- Connector OAuth credentials stored in tokens with public callback support (030be64)
- Task execution now propagates UserID through the full pipeline (71850e9)
- Mobile: pinned Tasks session in drawer, simplified notification list, back navigation (5bc9adf, 9f088cd, 791e054)

### Fixes

- Tasks chat: session fetch was casting SessionDetail as Session, making pinned entry invisible (1209de4)
- Notification click now navigates to the Tasks chat session (c9e77cd)
- Unread notifications in bell dropdown have better contrast (5151272)
- Mobile notifications and settings screens have proper back button instead of trapping the user (791e054)
- Agent safely parses multimodal content blocks without crashing (e79dc5a)

## 0.12.0 (2026-03-06)

### Features

- Generic connector runtime: manifest parser, OAuth2, REST executor for pluggable service integrations (f09743a, 8a56db5, 5fa066a)
- Google Calendar and Gmail connector shipped as embedded default with OAuth flow (a1a8716, 3c3939f, 2d4c536)
- Multi-calendar support: choose which calendars to query in connector settings modal
- Parallel fan-out across enabled calendars with dedup, sorting, and source tagging per event
- Auto-fetch calendar list on OAuth connect so settings are populated immediately
- Connector settings API (GET/PUT/POST refresh) with YAML-backed per-user storage
- Verified badge on built-in connectors to distinguish from user-added ones

### Fixes

- Light theme: unread notification text, connector status badge, and disconnect button now use proper contrast colors
- macOS desktop: theme and user preferences persist across app updates (localStorage no longer wiped on relaunch)

## 0.11.0 (2026-03-05)

### Features

- File attachments in chat: upload PDFs, images, Word docs, and text files across dashboard, mobile, and macOS desktop (7f27eaf, e083859, 00c21f7, 9dd2ff1, 898b00c)
- Agent automatically reads and incorporates attached file content without explicit prompting (564d572)
- Session deletion synced across all connected devices in real time (88ebcca)
- Paperclip icon for the attachment button on dashboard and mobile (1d37782)
- Desktop app icon updated to match mobile logo (455e693)

### Fixes

- macOS WKWebView now shows the native file picker for uploads (2b1dd97)
- File upload responses display correctly on dashboard and mobile instead of spinning indefinitely (d6a2a91, c09f1bc)
- File attachment button works without an active session selected (f51bed0)
- Multimodal messages render as readable text instead of raw JSON on synced devices (c09f1bc)
- Agent no longer disclaims about missing file content (b7dadad)
- REST file upload endpoint defaults channel to "web" so sessions appear under Chats (d6a2a91)
- Mobile attach button height matches send button (05a4128)
- Expo Go push notification warnings suppressed via lazy module loading (8a73812, d263cfd)
- Dashboard synchronous setState warning in models section resolved (89fdfc9)

## 0.10.0 (2026-03-05)

### Features

- Push notifications via Expo Push API with automatic invalid token cleanup (8fe19ee, a29e2f0, 7f1abe7, 72f785d)
- Mobile push: token registration, notification tap handling, badge management, and settings toggle (4644332, 70f684f, 924f0ab)
- Notification deletion: single and bulk delete across dashboard and mobile (b26b9fa)
- Broadcast flag on tasks to send completion notifications to all users (63b67b5)

### Fixes

- Push notifications now delivered regardless of active WebSocket connections (9475db4)
- Foreground notification display on iOS (9475db4)
- Lazy-init push module to prevent crash in native iOS builds (9ad8ff9)
- Agent-created tasks now assigned to the requesting user (c00f220)
- Cross-device notification badge sync on read events (fe8863a, 67ee2f1)

### Improvements

- Dashboard notifications UI aligned with tasks table layout (d92d44f, b81a6f8)

## 0.9.0 (2026-03-05)

### Features

- Persistent notification feed backed by SQLite, replacing ephemeral session-based task notifications (a313143)
- Notification REST API: list with pagination, mark read, mark all read (a313143)
- Dashboard notifications page with markdown rendering, unread dot indicators, and HUD-style corner bracket accents (a313143)
- Mobile notifications screen with markdown rendering, pull-to-refresh, and unread badge in session drawer (a313143)
- Trigger labels display human-readable names: Scheduled, Manual, Webhook, Chat (a313143)

### Fixes

- Legacy notifications (NULL user_id from pre-multi-user tasks) now visible to authenticated users (a313143)
- Notification table migration no longer uses FK constraint on user_id, preventing insert failures in single-user mode (a313143)

## 0.8.0 (2026-03-05)

### Features

- Multi-user authentication with JWT tokens, refresh tokens, and role-based access control (8683526, d681a7d, 8c0e67b, 1383700, 227125a, 8852873)
- Admin bootstrap from environment variables on first run (cbefa02)
- User-scoped sessions, tasks, memory, workers, and WebSocket connections (405a9fb, 84698c9, 3d43945, 94fc8c0, 7f226c3, 9a170d6, 3bf0b88, 7d5a707, 1f90ab6)
- Per-user profile overrides merged into system prompt (28fc465)
- Dashboard auth UI with login, registration, admin panel, and desktop JWT flow (5fadbe3, adc32c3)
- Account page for user profile management (b7bc8b5)
- Private sessions visible only to their owner (b7bc8b5)
- Relay tunnel system for remote access via WebSocket proxy (6c4fcac, e97c816, 4969f14)
- Mobile app: full auth flow with invite codes, registration, login, and JWT lifecycle (c0d3798, 65edb3a, b856985, 4eb5d99, c21c5c8, 00c22d5, 83ddd35)
- Mobile app: base64 invite codes generated from Admin page (6723120)
- macOS desktop: onboarding flow with server/client mode selection (78f342d)
- macOS desktop: client mode connects to remote Cogitator via invite code (78f342d)
- macOS desktop: Switch Mode menu item to return to onboarding (78f342d)
- Models configuration moved from Settings to Admin page (78f342d)

### Fixes

- Ownership checks on all resource endpoints (80b61bf)
- Relay token persistence, list response wrapping, and WebSocket proxying (07e33bb)
- Relay connection status polling and stop/restart lifecycle (db3b861, b7bdc59)
- Path-based relay routing for invite URLs (609d3c8)
- Admin UI style alignment and password reset for all users (7f25cd7)
- Mobile logout server call wrapped in try/catch (011f820)
- Makefile version detection excludes mobile tags to prevent sed failures in app builds

### Improvements

- Multi-user integration test suite (d1fb84e)
- Sidebar nav reorder: Account and Settings moved to bottom (78f342d)

## 0.7.1 (2026-03-04)

### Features

- `heal_task` tool for manual self-healing of tasks with wrong output (21b8e7c)
- Provider HTTP retry with exponential backoff for timeouts, 429, and 5xx (21b8e7c)
- Mobile: welcome screen with suggestion chips on empty chat (cf2ec43)

### Fixes

- Mobile: settings health check now tests the URL being edited (cf2ec43)

## 0.7.0 (2026-03-03)

### Features

- Mobile: message long-press actions with copy to clipboard and delete (0035bc5)
- Mobile: corner bracket accents on chat bubbles and wider message width (7f599a7)
- Mobile: scroll-to-bottom FAB and pull-to-refresh on chat (9905ad8)
- Mobile: haptic feedback on send, receive, and drawer interactions (d11cd36)
- Mobile: fade and slide entry animations on messages (7a736e9)
- Mobile: swipe-to-delete sessions in drawer (1250504)
- Mobile: upgrade to Expo SDK 54 with React Native 0.81 (e256b51)
- Mobile: EAS Build pipeline with GitHub Actions for TestFlight (cd954c1)
- Accept ClawHub URLs as skill slugs in install flows (c2ecbe5)
- Cycling activity labels for running tasks (38e3e0a)

### Fixes

- Mobile: Android swipe gestures for drawer open and session delete (bb8f295)
- Mobile: drawer layout, settings icon placement, send button height (e59d4a8)
- Mobile: keyboard handling, spinner style, drawer padding (ce975d8)
- Mobile: hooks violation and WebSocket origin allowlist (8e4da32)
- Live-tick duration counter for running tasks in history view (072b86a)
- Tabular-nums for consistent digit widths across the dashboard (d771649)

## 0.6.0 (2026-03-02)

### Features

- Memory System v2: vector embedding retrieval with cosine similarity and recency boost, replacing LLM-based classification for scalable semantic search (d9ba4d9, 6b3d1a2)
- Pinned memories: critical facts (name, timezone, preferences) are always present in agent context; pin/unpin via API, dashboard toggle, or agent auto-pin on save (08a5e13, 7a74567)
- Memory consolidation: background worker clusters related episodes into pattern nodes with adaptive thresholds that consolidate aggressively early and scale up as the knowledge base matures (5587cd6)
- Profile regeneration triggers after every N new memories instead of only on a daily cron schedule (07fdef4)
- Embedding backfill: existing memory nodes without embeddings are automatically embedded on startup (2af1e2a)

### Fixes

- Update banner no longer shows for dev builds at the same base version, e.g. v0.5.2-dirty no longer triggers an update to v0.5.2 (49e573e)
- Chat UI refreshes immediately after enabling Telegram in Settings instead of requiring a restart (4c2988d)
- Memory page "Pinned" filter no longer returns a 400 error (b2e6388)

### Improvements

- Embedding reuses the standard provider's connection instead of requiring a separate embedding provider configuration (5eaf05e)
- Memory node list API accepts an optional type filter, enabling cross-type queries (b2e6388)

## 0.5.2 (2026-03-02)

### Fixes

- SaveSecrets now merges into the existing secrets.yaml instead of overwriting it, preventing the GitHub token and MCP credentials from being wiped on settings save
- Updater loadCache revalidates UpdateAvailable against the current version, fixing stale update banner after upgrades
- Updater logs a warning when GitHub returns 404 for a repo that previously had releases, surfacing missing token issues for private repos

## 0.5.1 (2026-03-02)

### Features

- Chat welcome screen with suggestion chips that send messages on click, replacing the empty "No messages yet" placeholder to help new users discover capabilities (Chat.tsx)
- "What You Can Do" section in agent system prompt so the agent can describe its features conversationally when asked (context.go)

### Improvements

- Agent follows delete-and-recreate workflow for task updates instead of creating duplicates (context.go, builtin.go)
- Agent prefers update_skill over create_skill when modifying existing skills (context.go, builtin.go)
- Agent self-heals broken skills and task prompts during execution without waiting for user reports (context.go, builtin.go)
- Broader update_skill tool description covering both user-requested changes and autonomous fixes

## 0.5.0 (2026-03-02)

### Features

- Native macOS desktop notifications with page-aware suppression: notifications are only shown when the dashboard tab is not visible (c733b72)
- Developer ID code signing with hardened runtime, entitlements for WebKit and networking, inside-out signing replacing deprecated `--deep` flag (3fd7d3d)
- Apple notarization support via `make notarize` target, gated on `APPLE_TEAM_ID` so ad-hoc local builds still work (3fd7d3d)
- Makefile loads signing variables from optional `.env` file (3fd7d3d)

### Fixes

- Task executor now reads the current model from the config store instead of a startup snapshot, so changing the model in settings takes effect immediately for scheduled tasks (fe8bafc)
- Auto-updater no longer re-signs downloaded bundles with ad-hoc identity, preserving the Developer ID signature and notarization ticket (3fd7d3d)

## 0.4.0 (2026-03-02)

### Features

- Server-side tool name resolution: assistant messages now carry pre-resolved, human-readable tool activity grouped into Skills, Tools, and Memory categories
- `start_mcp_server` builtin tool allowing the agent to explicitly start configured MCP servers by name
- Lazy-start fallback for unregistered MCP tools: `mcp__server__tool` calls now auto-start the server and route the call without prior registration
- Tool activity badges displayed on assistant messages in the dashboard chat UI
- Persisted tool usage in chat history via new `tools_used` column on the messages table

### Improvements

- Channel `MessageHandler` now returns a structured `HandlerResponse` instead of a plain string, carrying both content and resolved tool metadata
- `MCPManager` interface extended with `StartServer()` and `ServerNames()` methods for explicit server lifecycle control
- Dashboard chat message parsing deduplicated into a single `toChatMessages()` helper

## 0.3.2 (2026-03-01)

### Fixes

- Fix update banner showing when current version matches latest due to "v" prefix mismatch (b180eea)
- Use a separate 10-minute HTTP client for asset downloads instead of the 15-second API client (b180eea)
- Clear "Downloading..." state in the dashboard when the server reports a download error (b180eea)

## 0.3.1 (2026-03-01)

### Features

- Skip version button in the update banner; preference persists across restarts via cogitator.yaml
- Cache latest release info to disk so the update banner appears instantly on restart

### Fixes

- Fix update banner scrolled out of view by Chat auto-scroll cascading to parent containers
- Restructure dashboard layout so banners compress the content area instead of scrolling within it

## 0.3.0 (2026-03-01)

### Features

- MCP server lifecycle management with config persistence, tool discovery, and lazy start (c6e55f1)
- REST API for MCP server management and tool testing (5a422ff)
- Dedicated MCP dashboard page with server controls and interactive tool testing (8a517fc)
- WebSocket forwarding of MCP server state changes (afba61c)
- MCP remote transport support for SSE and Streamable HTTP servers (7ac6a8d)
- Auto-reconnect with exponential backoff for remote MCP servers (01a7571)
- Secrets and OAuth management for remote MCP server authentication (f9e6cd8)
- Tabbed Add Server modal with Local and Remote modes (a2d7a91)
- Per-server instructions field for describing MCP server purpose (206f05f)
- Enriched MCP tool descriptions with server context for better LLM tool selection (e79c976)
- Auto-generated skill per MCP server with full tool catalog and input schemas (c91bf4c)
- System prompt hint nudging the agent to discover MCP server skills (18de384)
- Dashboard UI for editing MCP server instructions inline (50c3716)
- PATCH endpoint for updating MCP server instructions (bb10688)

### Fixes

- Sanitize MCP tool names for LLM provider API compatibility; server names with dots or spaces no longer cause 400 errors (797f564)
- Chat input text now uses theme-aware color instead of hardcoded black (bd87ee0)
- Reset form submitting state on early validation return in Add Server modal (7ca2aa3)
- MCP skill content now refreshes on every tool discovery instead of going stale (54dfb83)

## 0.2.0 (2026-02-27)

### Features

- Per-launch bearer token authentication for the localhost API, protecting against browser CSRF and local process abuse (01ea90c)
- CORS middleware restricts cross-origin requests to the server's own localhost origin (01ea90c)
- WebSocket origin validation tightened from accept-all to localhost-only patterns (01ea90c)
- Swift wrapper generates a cryptographic token per launch and injects it into the WKWebView (01ea90c)

## 0.1.4 (2026-02-27)

### Fixes

- Fix Swift wrapper not quitting when server exits cleanly during auto-update
- Fix stale download state persisting after extracted files are removed from disk

## 0.1.3 (2026-02-27)

### Improvements

- Two-phase update UX: "Update Now" downloads in the background, then a "Restart" button appears when ready
- Server process now exits cleanly during update so the swap script can proceed

### Fixes

- Fix update process hanging because server did not terminate after shutdown

## 0.1.2 (2026-02-27)

### Fixes

- Fix auto-updater using wrong GitHub repository name, preventing update detection

## 0.1.1 (2026-02-27)

### Fixes

- Fix double "v" prefix in dashboard version display in sidebar and update banner

## 0.1.0 (2026-02-27)

### Features

- Self-learning personal AI agent with event-driven Go server and SQLite storage
- Conversational agent with multi-round tool use, dynamic context compression, and behavioral profiling
- Knowledge graph memory with node/edge storage, content sharding, and async LLM enrichment
- Two-stage associative memory retrieval with keyword and semantic search
- Cron-based task scheduler with retry, backoff, cancellation, and run history
- ClawHub skill marketplace integration (search, install, uninstall) and learned skills
- Sandboxed tool execution with host and Docker modes
- Security: path validation, dangerous command blocking, domain allowlists
- OpenAI-compatible LLM provider abstraction with hot-swap support
- Ollama integration for local models (pull, list, delete, model selection)
- WebSocket web chat channel
- Telegram bot channel
- React/TypeScript dashboard with Chat, Tasks, Memory, Skills, History, Resources, and Settings pages
- Token usage charts and memory graph visualization
- macOS desktop app with native Swift/Cocoa wrapper and embedded dashboard
- Universal binary support (arm64 + amd64) for macOS
- Auto-update system checking GitHub releases with one-click install
- Private repository support for auto-updates via GitHub token
- React Native/Expo mobile chat client for Android

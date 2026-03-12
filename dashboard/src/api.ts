import React from 'react';

const BASE = '';

declare global {
  interface Window {
    __COGITATOR_AUTH_TOKEN__?: string;
  }
}

// Token provider: set by AuthProvider, used by authHeaders().
let _getToken: (() => string | null) | null = null;
let _refreshFn: (() => Promise<boolean>) | null = null;

export function setTokenProvider(fn: () => string | null) { _getToken = fn; }
export function setRefreshHandler(fn: () => Promise<boolean>) { _refreshFn = fn; }

export function authHeaders(): Record<string, string> {
  const token = _getToken?.() ?? window.__COGITATOR_AUTH_TOKEN__;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/** Perform a fetch, and on 401 attempt a token refresh + retry once. */
async function fetchWithRetry(input: string, init?: RequestInit): Promise<Response> {
  let res = await fetch(input, init);
  if (res.status === 401 && _refreshFn) {
    const ok = await _refreshFn();
    if (ok) {
      // Rebuild headers with new token.
      const retryInit = { ...init, headers: { ...init?.headers, ...authHeaders() } };
      res = await fetch(input, retryInit);
    }
  }
  return res;
}

export async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetchWithRetry(BASE + path, { headers: { ...authHeaders() } });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetchWithRetry(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetchWithRetry(BASE + path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function patchJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetchWithRetry(BASE + path, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function deleteJSON(path: string): Promise<void> {
  const res = await fetchWithRetry(BASE + path, { method: 'DELETE', headers: { ...authHeaders() } });
  if (!res.ok && res.status !== 204) {
    throw new Error(`API ${res.status}: ${await res.text()}`);
  }
}

export function fetchOllamaStatus(): Promise<OllamaStatus> {
  return fetchJSON('/api/ollama/status');
}

export function fetchOllamaModels(): Promise<OllamaModelsResponse> {
  return fetchJSON('/api/ollama/models');
}

export function deleteOllamaModel(name: string): Promise<void> {
  return deleteJSON(`/api/ollama/models/${encodeURIComponent(name)}`);
}

// Polling hook helper. Uses a ref for the fetcher so the effect restarts only
// when intervalMs or fetchKey change, but always calls the latest fetcher.
export function usePolling<T>(
  fetcher: () => Promise<T>,
  intervalMs: number,
  fetchKey?: string,
): { data: T | null; error: string | null; loading: boolean; refresh: () => void } {
  const [data, setData] = React.useState<T | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(true);
  const fetcherRef = React.useRef(fetcher);
  const lastJsonRef = React.useRef<string>('');
  fetcherRef.current = fetcher;

  const poll = React.useCallback(async () => {
    try {
      const result = await fetcherRef.current();
      const json = JSON.stringify(result);
      if (json !== lastJsonRef.current) {
        lastJsonRef.current = json;
        setData(result);
      }
      setError(null);
      setLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    let active = true;
    setLoading(true);
    const wrappedPoll = async () => {
      if (active) await poll();
    };
    wrappedPoll();
    const id = setInterval(wrappedPoll, intervalMs);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [intervalMs, fetchKey, poll]);

  return { data, error, loading, refresh: poll };
}

// Types matching the Go API

export interface SystemStatus {
  uptime_seconds: number;
  go_version: string;
  goroutines: number;
  provider_configured: boolean;
  memory: {
    alloc_mb: number;
    total_alloc_mb: number;
    sys_mb: number;
    num_gc: number;
  };
  components: {
    sessions: number;
    memory_nodes: number;
    tasks: number;
    tools: number;
    skills: number;
  };
}

export interface Task {
  id: number;
  name: string;
  prompt: string;
  cron_expr: string;
  model_tier: string;
  enabled: boolean;
  max_retries: number;
  retry_backoff: number;
  timeout: number;
  working_dir: string;
  notify: string;
  allow_manual: boolean;
  notify_chat: boolean;
  broadcast: boolean;
  created_at: string;
  created_by: string;
  updated_at: string;
  next_run_at?: string;
  cron_description?: string;
  total_runs: number;
  last_status?: string;
}

export interface ToolCallRecord {
  tool: string;
  error?: string;
  duration_ms: number;
  round: number;
}

export interface Run {
  id: number;
  task_id: number;
  task_name: string;
  trigger: string;
  status: string;
  model_used: string;
  started_at: string;
  finished_at: string;
  duration_ms: number;
  result_summary: string;
  error_message: string;
  error_class: string;
  transcript_path: string;
  skills_used: string;
  session_key: string;
  tool_calls?: ToolCallRecord[];
}

export interface RunListResult {
  runs: Run[];
  total: number;
}

export interface MemoryNode {
  id: string;
  type: string;
  title: string;
  summary: string;
  tags: string[];
  retrieval_triggers: string[];
  confidence: number;
  content_path: string;
  enrichment_status: string;
  origin: string;
  source_url: string;
  version: string;
  skill_path: string;
  pinned: boolean;
  created_at: string;
  updated_at: string;
  last_accessed: string | null;
  subject_id?: string;
  user_id?: string;
}

export interface MemoryStats {
  total_nodes: number;
  [key: string]: number;
}

export interface MemoryNodeSummary {
  id: string;
  type: string;
  title: string;
  summary: string;
}

export interface MemoryEdge {
  source_id: string;
  target_id: string;
  relation: string;
  weight: number;
}

export interface MemoryGraph {
  nodes: MemoryNodeSummary[];
  edges: MemoryEdge[];
}

export interface ModelSettings {
  provider: string;
  model: string;
}

export interface ProviderSettings {
  api_key_set: boolean;
}

export interface TelegramSettings {
  enabled: boolean;
  bot_token_set: boolean;
  allowed_chat_ids: number[];
}

export interface Settings {
  workspace: { path: string };
  models: {
    standard: ModelSettings;
    cheap: ModelSettings;
  };
  providers: Record<string, ProviderSettings>;
  telegram: TelegramSettings;
  security: { allowed_domains: string[] };
  server: { public_url: string };
}

export interface ModelUpdateRequest {
  provider?: string;
  model?: string;
}

export interface ProviderUpdateRequest {
  api_key: string;
}

export interface TelegramUpdateRequest {
  enabled?: boolean;
  bot_token?: string;
  allowed_chat_ids?: number[];
}

export interface SettingsUpdateRequest {
  workspace?: { path: string };
  models?: {
    standard?: ModelUpdateRequest;
    cheap?: ModelUpdateRequest;
  };
  providers?: Record<string, ProviderUpdateRequest>;
  telegram?: TelegramUpdateRequest;
  security?: { allowed_domains?: string[] };
  server?: { public_url?: string };
}

// Ollama (local models)

export interface OllamaModel {
  name: string;
  size: number;
  family: string;
  parameter_size: string;
  quantization_level: string;
  modified_at: string;
}

export interface OllamaStatus {
  running: boolean;
}

export interface OllamaModelsResponse {
  models: OllamaModel[];
}

export interface DailyTokenStats {
  date: string;
  model_tier: string;
  tokens_in: number;
  tokens_out: number;
}

export function pinMemoryNode(id: string, pinned: boolean): Promise<MemoryNode> {
  return patchJSON<MemoryNode>(`/api/memory/nodes/${encodeURIComponent(id)}/pin`, { pinned });
}

export function fetchSkillContent(id: string): Promise<{ content: string }> {
  return fetchJSON(`/api/skills/nodes/${encodeURIComponent(id)}/content`);
}

export function updateSkill(
  id: string,
  body: { title?: string; summary?: string; content?: string },
): Promise<MemoryNode> {
  return putJSON(`/api/skills/nodes/${encodeURIComponent(id)}`, body);
}

export function importSkill(content: string): Promise<{ node_id: string; name: string; slug: string }> {
  return postJSON('/api/skills/import', { content });
}

export function fetchDailyTokenStats(days = 14): Promise<{ stats: DailyTokenStats[] }> {
  return fetchJSON(`/api/usage/daily?days=${days}`);
}

// Version / auto-update

export interface VersionReleaseInfo {
  tag: string;
  name: string;
  url: string;
  asset_url: string;
  asset_name: string;
  published_at: string;
}

export interface VersionInfo {
  current: string;
  latest?: VersionReleaseInfo;
  update_available: boolean;
  can_auto_update: boolean;
  checking: boolean;
  downloading: boolean;
  ready: boolean;
  error?: string;
  skipped_version?: string;
}

export function fetchVersionInfo(): Promise<VersionInfo> {
  return fetchJSON('/api/version');
}

export function downloadUpdate(): Promise<VersionInfo> {
  return postJSON('/api/version/download', {});
}

export function restartUpdate(): Promise<{ status: string }> {
  return postJSON('/api/version/restart', {});
}

export function skipVersion(version: string): Promise<VersionInfo> {
  return postJSON('/api/version/skip', { version });
}

export interface Session {
  key: string;
  channel: string;
  chat_id: string;
  summary?: string;
  private?: boolean;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  last_active: string;
}

export interface PersistedMessage {
  id: number;
  session_key: string;
  role: string;
  content: string;
  tool_calls?: string;
  tools_used?: string;
  created_at: string;
}

export interface SessionDetail {
  session: Session;
  messages: PersistedMessage[] | null;
}

export interface SkillMeta {
  slug: string;
  displayName: string;
  summary: string;
  version: string;
  score?: number;
  updatedAt?: number;
}

export interface SkillSearchResult {
  results: SkillMeta[];
}

export interface SkillDetail {
  skill: {
    slug: string;
    displayName: string;
    summary: string;
    tags: Record<string, string>;
    stats: {
      downloads: number;
      installsAllTime: number;
      installsCurrent: number;
      stars: number;
      versions: number;
      comments: number;
    };
    createdAt: number;
    updatedAt: number;
  };
  latestVersion: {
    version: string;
    changelog: string;
    createdAt: number;
  };
  owner: {
    handle: string;
    displayName: string;
  };
}

// Connectors

export type ConnectorInfo = {
  name: string;
  display_name: string;
  description: string;
  version: string;
  has_auth: boolean;
  connected: boolean;
  trusted: boolean;
};

export function fetchConnectors(): Promise<ConnectorInfo[]> {
  return fetchJSON('/api/connectors');
}

export function fetchConnectorStatus(name: string): Promise<{ connected: boolean }> {
  return fetchJSON(`/api/connectors/${encodeURIComponent(name)}/status`);
}

export function startConnectorAuth(name: string): Promise<{ url: string }> {
  return fetchJSON(`/api/connectors/${encodeURIComponent(name)}/auth/start`);
}

export async function disconnectConnector(name: string): Promise<void> {
  return deleteJSON(`/api/connectors/${encodeURIComponent(name)}/auth`);
}

export type CalendarEntry = {
  id: string;
  summary: string;
  primary: boolean;
};

export type ConnectorSettings = {
  calendars: CalendarEntry[];
  enabled_calendar_ids: string[];
};

export function fetchConnectorSettings(name: string): Promise<ConnectorSettings> {
  return fetchJSON(`/api/connectors/${encodeURIComponent(name)}/settings`);
}

export function updateConnectorSettings(
  name: string,
  enabledCalendarIDs: string[],
): Promise<void> {
  return putJSON(`/api/connectors/${encodeURIComponent(name)}/settings`, {
    enabled_calendar_ids: enabledCalendarIDs,
  });
}

export function refreshConnectorSettings(name: string): Promise<ConnectorSettings> {
  return postJSON(`/api/connectors/${encodeURIComponent(name)}/settings/refresh`, {});
}

// MCP (Model Context Protocol)

export interface MCPServer {
  name: string;
  status: 'stopped' | 'starting' | 'running' | 'reconnecting' | 'error';
  command: string;
  args: string[];
  url: string;
  transport: string;
  remote: boolean;
  tool_count: number;
  started_at: string | null;
  error: string | null;
  instructions: string;
}

export interface MCPTool {
  name: string;
  qualified_name: string;
  description: string;
  input_schema: Record<string, unknown>;
}

export interface MCPToolTestResult {
  result: string;
  duration_ms: number;
  error: string | null;
}

export function fetchMCPServers(): Promise<{ servers: MCPServer[] }> {
  return fetchJSON('/api/mcp/servers');
}

export function addMCPServer(body: {
  name: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  transport?: string;
  headers?: Record<string, string>;
  instructions?: string;
}): Promise<{ status: string }> {
  return postJSON('/api/mcp/servers', body);
}

export interface MCPServerSecretsRequest {
  headers?: Record<string, string>;
  oauth?: {
    client_id: string;
    client_secret: string;
    scopes?: string[];
    redirect_uri?: string;
  };
}

export function updateMCPServerSecrets(
  name: string,
  body: MCPServerSecretsRequest,
): Promise<{ status: string }> {
  return putJSON(`/api/mcp/servers/${encodeURIComponent(name)}/secrets`, body);
}

export function updateMCPServer(
  name: string,
  body: { instructions?: string },
): Promise<{ status: string }> {
  return patchJSON(`/api/mcp/servers/${encodeURIComponent(name)}`, body);
}

export function removeMCPServer(name: string): Promise<void> {
  return deleteJSON(`/api/mcp/servers/${encodeURIComponent(name)}`);
}

export function startMCPServer(name: string): Promise<{ status: string }> {
  return postJSON(`/api/mcp/servers/${encodeURIComponent(name)}/start`, {});
}

export function stopMCPServer(name: string): Promise<{ status: string }> {
  return postJSON(`/api/mcp/servers/${encodeURIComponent(name)}/stop`, {});
}

export function fetchMCPTools(serverName: string): Promise<{ tools: MCPTool[] }> {
  return fetchJSON(`/api/mcp/servers/${encodeURIComponent(serverName)}/tools`);
}

export function testMCPTool(
  serverName: string,
  toolName: string,
  args: Record<string, unknown>,
): Promise<MCPToolTestResult> {
  return postJSON(
    `/api/mcp/servers/${encodeURIComponent(serverName)}/tools/${encodeURIComponent(toolName)}/test`,
    { arguments: args },
  );
}

export async function sendChatMessage(
  message: string,
  sessionKey: string,
  chatId: string,
  file?: File,
  isPrivate?: boolean,
): Promise<{ content: string; session_key: string; tools_used?: Record<string, string[]> }> {
  const form = new FormData();
  form.append('message', message);
  form.append('session_key', sessionKey);
  form.append('chat_id', chatId);
  if (file) form.append('file', file);
  if (isPrivate) form.append('private', 'true');

  const res = await fetch(`${BASE}/api/chat/message`, {
    method: 'POST',
    headers: authHeaders(),
    body: form,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || res.statusText);
  }
  return res.json();
}

// Auth / User / Invite types

export type UserRole = 'admin' | 'moderator' | 'user';

export interface User {
  id: string;
  email: string;
  name: string;
  role: UserRole;
  profile_overrides: string;
  has_password: boolean;
  created_at: string;
  updated_at: string;
}

export interface InviteCode {
  code: string;
  created_by: string;
  role: UserRole;
  redeemed_by: string | null;
  expires_at: string | null;
  created_at: string;
}

export interface AuthResponse {
  user?: User;
  access_token: string;
  refresh_token: string;
}

// Auth API (these bypass the 401 retry to avoid loops)

export async function loginAPI(email: string, password: string): Promise<AuthResponse> {
  const res = await fetch(BASE + '/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function fetchNeedsSetup(): Promise<{ needs_setup: boolean }> {
  const res = await fetch(BASE + '/api/auth/needs-setup');
  if (!res.ok) return { needs_setup: false };
  return res.json();
}

export async function setupAPI(
  email: string,
  name: string,
  password: string,
): Promise<AuthResponse> {
  const res = await fetch(BASE + '/api/auth/setup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, name, password }),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function registerAPI(
  email: string,
  name: string,
  password: string,
  inviteCode: string,
): Promise<AuthResponse> {
  const res = await fetch(BASE + '/api/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, name, password, invite_code: inviteCode }),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export async function socialLoginAPI(
  provider: string,
  idToken: string,
  inviteCode?: string,
): Promise<AuthResponse> {
  const res = await fetch(BASE + '/api/auth/social', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ provider, id_token: idToken, invite_code: inviteCode }),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export interface AuthProviders {
  google: boolean;
  google_client_id?: string;
  apple: boolean;
}

export function fetchAuthProviders(): Promise<AuthProviders> {
  return fetchJSON('/api/auth/providers');
}

export interface OAuthLink {
  id: string;
  user_id: string;
  provider: string;
  subject: string;
  email: string;
  created_at: string;
}

export function listOAuthLinks(): Promise<OAuthLink[]> {
  return fetchJSON('/api/account/links');
}

export async function linkOAuth(provider: string, idToken: string): Promise<void> {
  const res = await fetchWithRetry(BASE + '/api/account/link', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ provider, id_token: idToken }),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
}

export function unlinkOAuth(provider: string): Promise<void> {
  return deleteJSON(`/api/account/link/${encodeURIComponent(provider)}`);
}

export async function refreshTokenAPI(refreshToken: string): Promise<AuthResponse> {
  const res = await fetch(BASE + '/api/auth/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });
  if (!res.ok) throw new Error(`API ${res.status}: ${await res.text()}`);
  return res.json();
}

export function logoutAPI(): Promise<void> {
  return deleteJSON('/api/auth/logout');
}

// Current user

export function fetchMe(): Promise<User> {
  return fetchJSON('/api/me');
}

export function updateMe(body: { current_password: string; name?: string; email?: string; password?: string }): Promise<User> {
  return putJSON('/api/me', body);
}

// User management (admin only)

export function fetchUsers(): Promise<{ users: User[] }> {
  return fetchJSON('/api/users');
}

export function updateUserRole(id: string, role: UserRole): Promise<User> {
  return putJSON(`/api/users/${encodeURIComponent(id)}/role`, { role });
}

export function resetUserPassword(id: string, password: string): Promise<void> {
  return putJSON(`/api/users/${encodeURIComponent(id)}/password`, { password });
}

export function deleteUser(id: string): Promise<void> {
  return deleteJSON(`/api/users/${encodeURIComponent(id)}`);
}

// Invite codes (admin + moderator)

export function createInviteCode(role: UserRole, expiresAt?: string): Promise<InviteCode> {
  const body: Record<string, string> = { role };
  if (expiresAt) body.expires_at = expiresAt;
  return postJSON('/api/invite-codes', body);
}

export function fetchInviteCodes(): Promise<{ codes: InviteCode[] }> {
  return fetchJSON('/api/invite-codes');
}

export function deleteInviteCode(code: string): Promise<void> {
  return deleteJSON(`/api/invite-codes/${encodeURIComponent(code)}`);
}

// Notifications

export interface NotificationItem {
  id: number;
  task_id?: number;
  task_name: string;
  run_id: number;
  trigger: string;
  status: string;
  content: string;
  read: boolean;
  created_at: string;
}

export interface NotificationListResponse {
  notifications: NotificationItem[];
  total: number;
  unread: number;
}

export function listNotifications(limit = 50, offset = 0): Promise<NotificationListResponse> {
  return fetchJSON(`/api/notifications?limit=${limit}&offset=${offset}`);
}

export function markNotificationRead(id: number): Promise<void> {
  return putJSON(`/api/notifications/${id}/read`, {});
}

export function markAllNotificationsRead(): Promise<void> {
  return putJSON('/api/notifications/read-all', {});
}

export function deleteNotification(id: number): Promise<void> {
  return deleteJSON(`/api/notifications/${id}`);
}

export function deleteAllNotifications(): Promise<void> {
  return deleteJSON('/api/notifications');
}

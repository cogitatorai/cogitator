import { useState, useCallback, useEffect } from 'react';
import { Link2, Unlink, Settings, ShieldCheck } from 'lucide-react';
import {
  usePolling,
  fetchConnectors,
  startConnectorAuth,
  disconnectConnector,
  fetchConnectorSettings,
  updateConnectorSettings,
  refreshConnectorSettings,
} from '../api';
import type { ConnectorInfo, CalendarEntry } from '../api';
import PageHeader from '../components/PageHeader';
import MCP from './MCP';

const GoogleIcon = () => (
  <svg width="16" height="16" viewBox="0 0 48 48" aria-hidden="true">
    <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/>
    <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/>
    <path fill="#FBBC05" d="M10.53 28.59a14.5 14.5 0 0 1 0-9.18l-7.98-6.19a24.0 24.0 0 0 0 0 21.56l7.98-6.19z"/>
    <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/>
  </svg>
);

export default function Connectors() {
  const { data: connectors, refresh } = usePolling<ConnectorInfo[]>(fetchConnectors, 5000);
  const [busy, setBusy] = useState<string | null>(null);
  const [pending, setPending] = useState<Record<string, 'connecting' | 'disconnecting'>>({});

  // Settings modal state.
  const [settingsFor, setSettingsFor] = useState<string | null>(null);
  const [calendars, setCalendars] = useState<CalendarEntry[]>([]);
  const [enabledIDs, setEnabledIDs] = useState<string[]>([]);
  const [settingsLoading, setSettingsLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  // Clear pending entries when poll confirms the status change.
  useEffect(() => {
    if (!connectors) return;
    setPending((prev) => {
      const next = { ...prev };
      for (const c of connectors) {
        if (c.connected && next[c.name] === 'connecting') delete next[c.name];
        if (!c.connected && next[c.name] === 'disconnecting') delete next[c.name];
      }
      return Object.keys(next).length === Object.keys(prev).length ? prev : next;
    });
  }, [connectors]);

  const handleConnect = async (name: string) => {
    setBusy(name);
    try {
      const { url } = await startConnectorAuth(name);
      setPending((prev) => ({ ...prev, [name]: 'connecting' }));
      window.location.href = url;
    } catch (e) {
      alert(e instanceof Error ? e.message : 'Failed to start auth');
    } finally {
      setBusy(null);
    }
  };

  const handleDisconnect = async (name: string) => {
    setBusy(name);
    try {
      await disconnectConnector(name);
      setPending((prev) => ({ ...prev, [name]: 'disconnecting' }));
      refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : 'Failed to disconnect');
    } finally {
      setBusy(null);
    }
  };

  const openSettings = useCallback(async (name: string) => {
    setSettingsFor(name);
    setSettingsLoading(true);

    try {
      // Show cached data immediately.
      const cached = await fetchConnectorSettings(name);
      setCalendars(cached.calendars);
      setEnabledIDs(cached.enabled_calendar_ids);
    } catch {
      // No cached data yet.
    }

    // Refresh from provider in background.
    try {
      const fresh = await refreshConnectorSettings(name);
      setCalendars(fresh.calendars);
      // Preserve user's enabled selection; only update if this is the first time.
      if (fresh.enabled_calendar_ids.length > 0) {
        setEnabledIDs(fresh.enabled_calendar_ids);
      }
    } catch {
      // Background refresh failed; cached data is fine.
    } finally {
      setSettingsLoading(false);
    }
  }, []);

  const toggleCalendar = (id: string) => {
    setEnabledIDs((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const saveSettings = async () => {
    if (!settingsFor) return;
    setSaving(true);
    try {
      await updateConnectorSettings(settingsFor, enabledIDs);
      setSettingsFor(null);
    } catch (e) {
      alert(e instanceof Error ? e.message : 'Failed to save');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-8">
      {/* OAuth Connectors */}
      <div>
        <PageHeader
          title="Connectors"
          subtitle="Connect external services to extend your assistant's capabilities."
        />

        {connectors && connectors.length > 0 ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 mt-4">
            {connectors.map((c) => {
              const p = pending[c.name];
              const statusLabel = p
                ? (p === 'connecting' ? 'Connecting...' : 'Disconnecting...')
                : c.connected ? 'Connected' : 'Not connected';
              const statusClass = p
                ? 'bg-orange-900/50 text-orange-500'
                : c.connected
                  ? 'bg-green-900/50 text-green-600'
                  : 'bg-zinc-700/50 text-zinc-500';
              const dotClass = p
                ? 'bg-orange-500 animate-pulse'
                : c.connected ? 'bg-green-500' : 'bg-zinc-500';

              return (
              <div
                key={c.name}
                className="connector-card rounded-lg border border-zinc-700/50 bg-zinc-800 p-4 flex flex-col gap-3"
              >
                <div className="flex items-center justify-between">
                  <div>
                    <div className="flex items-center gap-1.5">
                      {c.name === 'google' && <GoogleIcon />}
                      <h3 className="text-sm font-medium text-zinc-100">
                        {c.display_name}
                      </h3>
                      {c.trusted && (
                        <span title="Verified connector">
                          <ShieldCheck size={13} className="text-blue-400 shrink-0" />
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-zinc-400 mt-0.5">v{c.version}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <span
                      className={`inline-flex items-center gap-1.5 text-xs font-medium px-2 py-0.5 rounded-full ${statusClass}`}
                    >
                      <span className={`w-1.5 h-1.5 rounded-full ${dotClass}`} />
                      {statusLabel}
                    </span>
                  </div>
                </div>

                <p className="text-xs text-zinc-400 leading-relaxed">
                  {c.description}
                </p>

                <div className="mt-auto pt-2 border-t border-zinc-700/30 flex items-center justify-between">
                  {c.connected ? (
                    <button
                      onClick={() => handleDisconnect(c.name)}
                      disabled={busy === c.name}
                      className="flex items-center justify-center gap-1.5 text-xs font-medium px-3 py-1.5 rounded-md bg-red-950 text-red-500 hover:bg-red-900 disabled:opacity-50 transition-colors"
                    >
                      <Unlink size={12} />
                      {busy === c.name ? 'Disconnecting...' : 'Disconnect'}
                    </button>
                  ) : c.has_auth ? (
                    <button
                      onClick={() => handleConnect(c.name)}
                      disabled={busy === c.name}
                      className="flex items-center justify-center gap-1.5 text-xs font-medium px-3 py-1.5 rounded-md bg-orange-500/15 text-orange-400 hover:bg-orange-500/25 disabled:opacity-50 transition-colors"
                    >
                      <Link2 size={12} />
                      {busy === c.name ? 'Connecting...' : 'Connect'}
                    </button>
                  ) : (
                    <span className="text-xs text-zinc-500">No auth required</span>
                  )}
                  {c.connected && (
                    <button
                      onClick={() => openSettings(c.name)}
                      className="p-1 rounded hover:bg-zinc-700/50 text-zinc-400 hover:text-zinc-200 transition-colors"
                      title="Configure"
                    >
                      <Settings size={14} />
                    </button>
                  )}
                </div>
              </div>
              );
            })}
          </div>
        ) : connectors ? (
          <p className="text-sm text-zinc-500 mt-4">No connectors available.</p>
        ) : null}
      </div>

      {/* MCP Servers (reused from MCP page) */}
      <MCP />

      {/* Settings Modal */}
      {settingsFor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="w-full max-w-md border border-zinc-700 bg-zinc-900 shadow-2xl rounded-lg">
            <div className="flex items-center justify-between p-4 border-b border-zinc-700/50">
              <h2 className="text-sm font-medium text-zinc-100">
                {connectors?.find((c) => c.name === settingsFor)?.display_name ?? settingsFor} Settings
              </h2>
              <button
                onClick={() => setSettingsFor(null)}
                className="text-zinc-400 hover:text-zinc-200 text-lg leading-none"
              >
                &times;
              </button>
            </div>

            <div className="p-4">
              <p className="text-xs text-zinc-400 mb-3">
                Select which calendars to include when searching for events.
              </p>

              {settingsLoading && calendars.length === 0 ? (
                <p className="text-xs text-zinc-500 py-4 text-center">Loading calendars...</p>
              ) : calendars.length === 0 ? (
                <p className="text-xs text-zinc-500 py-4 text-center">No calendars found.</p>
              ) : (
                <div className="space-y-1 max-h-64 overflow-y-auto">
                  {calendars.map((cal) => (
                    <label
                      key={cal.id}
                      className="flex items-center gap-3 px-2 py-1.5 rounded hover:bg-zinc-800/80 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={enabledIDs.includes(cal.id)}
                        onChange={() => toggleCalendar(cal.id)}
                        className="rounded border-zinc-600 bg-zinc-800 text-orange-500 focus:ring-orange-500/30"
                      />
                      <span className="text-sm text-zinc-200 flex-1 truncate">
                        {cal.summary || cal.id}
                      </span>
                      {cal.primary && (
                        <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-zinc-700/50 text-zinc-400">
                          Primary
                        </span>
                      )}
                    </label>
                  ))}
                </div>
              )}
            </div>

            <div className="flex justify-end gap-2 p-4 border-t border-zinc-700/50">
              <button
                onClick={() => setSettingsFor(null)}
                className="text-xs font-medium px-3 py-1.5 rounded-md text-zinc-400 hover:text-zinc-200 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={saveSettings}
                disabled={saving}
                className="text-xs font-medium px-3 py-1.5 rounded-md bg-orange-500/15 text-orange-400 hover:bg-orange-500/25 disabled:opacity-50 transition-colors"
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

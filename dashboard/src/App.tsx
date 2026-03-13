import { useState, useEffect, useCallback, useMemo } from 'react';
import { WebSocketProvider, useWebSocket } from './ws';
import { ListTodo, History, Gauge, MessageSquare, Brain, Puzzle, Cable, Settings, Shield, LogOut, UserCircle, Users } from 'lucide-react';
import { fetchJSON, usePolling, fetchVersionInfo, downloadUpdate, restartUpdate, skipVersion, listNotifications, fetchNeedsSetup } from './api';
import { useTheme } from './useTheme';
import { requestPermissionOnFirstLaunch, sendNotification } from './hooks/useDesktopNotifications';
import AuthProvider, { useAuth } from './auth';
import type { SystemStatus, VersionInfo } from './api';

import Tasks from './pages/Tasks';
import HistoryPage from './pages/History';
import Resources from './pages/Resources';
import Chat from './pages/Chat';
import Memory from './pages/Memory';
import Skills from './pages/Skills';
import Connectors from './pages/Connectors';
import SettingsPage from './pages/Settings';
import Login from './pages/Login';
import Register from './pages/Register';
import SignUp from './pages/SignUp';
import Connect from './pages/Connect';
import Admin from './pages/Admin';
import UsersPage from './pages/Users';
import Account from './pages/Account';
import NotificationBell from './components/NotificationBell';

type Page = 'tasks' | 'history' | 'resources' | 'chat' | 'memory' | 'skills' | 'connectors' | 'settings' | 'account' | 'admin' | 'users' | 'register' | 'connect';

/** Strip leading "v" and any pre-release suffix so "v0.3.1" and "0.3.1" compare equal. */
function bareVersion(v: string): string {
  const s = v.startsWith('v') ? v.slice(1) : v;
  const dash = s.indexOf('-');
  return dash >= 0 ? s.slice(0, dash) : s;
}

/** Return true if version a is strictly newer than version b (semver comparison). */
function isNewer(a: string, b: string): boolean {
  const pa = bareVersion(a).split('.').map(Number);
  const pb = bareVersion(b).split('.').map(Number);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const na = pa[i] ?? 0;
    const nb = pb[i] ?? 0;
    if (na > nb) return true;
    if (na < nb) return false;
  }
  return false;
}

const PAGES = new Set<Page>(['tasks', 'history', 'resources', 'chat', 'memory', 'skills', 'connectors', 'settings', 'account', 'admin', 'users', 'register', 'connect']);

type NavItem = { id: Page; label: string; icon: React.ReactNode };

const BASE_NAV: NavItem[] = [
  { id: 'chat', label: 'Chat', icon: <MessageSquare size={16} /> },
  { id: 'tasks', label: 'Tasks', icon: <ListTodo size={16} /> },
  { id: 'history', label: 'Task History', icon: <History size={16} /> },
  { id: 'memory', label: 'Memory', icon: <Brain size={16} /> },
  { id: 'skills', label: 'Skills', icon: <Puzzle size={16} /> },
  { id: 'connectors', label: 'Connectors', icon: <Cable size={16} /> },
  { id: 'resources', label: 'Resources', icon: <Gauge size={16} /> },
  // Admin inserted here for admin users (see nav memo below)
  { id: 'account', label: 'Account', icon: <UserCircle size={16} /> },
  { id: 'settings', label: 'Settings', icon: <Settings size={16} /> },
];

const ADMIN_NAV_ITEM: NavItem = { id: 'admin', label: 'Admin', icon: <Shield size={16} /> };
const USERS_NAV_ITEM: NavItem = { id: 'users', label: 'Users', icon: <Users size={16} /> };

function readHash(): Page {
  const raw = window.location.hash.replace('#', '');
  const base = raw.split(/[/?]/)[0];
  return PAGES.has(base as Page) ? (base as Page) : 'chat';
}

/** Global listener for task lifecycle notifications (always mounted). */
function TaskNotificationListener({ onNotification }: { onNotification?: () => void }) {
  const { subscribe, unsubscribe } = useWebSocket();

  useEffect(() => {
    const tasksPage = `chat/${encodeURIComponent('tasks:output')}`;
    const listener = (data: { type: string; content?: string; error?: string; status?: string }) => {
      if (data.type === 'task_completed') {
        sendNotification('Task completed', data.content || 'A task finished successfully', { page: tasksPage });
      } else if (data.type === 'task_failed') {
        const body = data.error
          ? `${data.content}: ${data.error}`.slice(0, 120)
          : `${data.content || 'A task'} failed`;
        sendNotification('Task failed', body, { page: tasksPage });
      } else if (data.type === 'notification') {
        const title = data.status === 'failed' ? 'Task failed' : 'Task completed';
        sendNotification(title, data.status === 'failed' ? 'A task has failed' : 'A task has completed', { subtitle: data.content, page: tasksPage });
        onNotification?.();
      } else if (data.type === 'notifications_read') {
        onNotification?.();
      }
    };
    subscribe(listener);
    return () => { unsubscribe(listener); };
  }, [subscribe, unsubscribe, onNotification]);

  return null;
}

export default function App() {
  return (
    <AuthProvider>
      <AppShell />
    </AuthProvider>
  );
}

function AppShell() {
  const [, themePreference, setTheme] = useTheme();
  const { user, loading: authLoading, isAuthenticated, isAdmin, isModerator, logout } = useAuth();
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);

  useEffect(() => {
    if (authLoading || isAuthenticated) return;
    fetchNeedsSetup().then((r) => setNeedsSetup(r.needs_setup)).catch(() => setNeedsSetup(false));
  }, [authLoading, isAuthenticated]);

  // On first launch, prompt for notification permission before user navigates.
  useEffect(() => { requestPermissionOnFirstLaunch(); }, []);

  const [page, setPage] = useState<Page>(readHash);
  const [unreadCount, setUnreadCount] = useState(0);
  const [notifRefreshKey, setNotifRefreshKey] = useState(0);

  // Fetch initial unread count.
  useEffect(() => {
    if (!isAuthenticated) return;
    listNotifications(1, 0).then((d) => setUnreadCount(d.unread)).catch(() => {});
  }, [isAuthenticated]);

  const handleNotification = useCallback(() => {
    listNotifications(1, 0).then((d) => setUnreadCount(d.unread)).catch(() => {});
    setNotifRefreshKey((k) => k + 1);
  }, []);

  // Guard: redirect unauthorized users away from restricted pages.
  useEffect(() => {
    if (!authLoading && isAuthenticated) {
      if (page === 'admin' && !isAdmin) {
        window.location.hash = 'chat';
        setPage('chat');
      }
      if (page === 'users' && !isAdmin && !isModerator) {
        window.location.hash = 'chat';
        setPage('chat');
      }
    }
  }, [page, authLoading, isAuthenticated, isAdmin, isModerator]);

  const nav = useMemo(() => {
    const items = [...BASE_NAV];
    const accountIdx = items.findIndex((i) => i.id === 'account');
    // Users page visible to admin and moderator.
    if (isAdmin || isModerator) {
      items.splice(accountIdx, 0, USERS_NAV_ITEM);
    }
    // Admin page visible to admin only (inserted after Users if present).
    if (isAdmin) {
      const insertIdx = items.findIndex((i) => i.id === 'users');
      items.splice(insertIdx >= 0 ? insertIdx + 1 : accountIdx, 0, ADMIN_NAV_ITEM);
    }
    return items;
  }, [isAdmin, isModerator]);

  const { data: status } = usePolling<SystemStatus>(
    () => isAuthenticated ? fetchJSON('/api/status') : Promise.resolve(null as unknown as SystemStatus),
    5000,
  );

  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);
  const [localDownloading, setLocalDownloading] = useState(false);

  const refreshVersion = useCallback(async () => {
    try {
      setVersionInfo(await fetchVersionInfo());
    } catch { /* ignore fetch errors */ }
  }, []);

  // Fetch on mount, again at 8s to catch the server's initial GitHub
  // check (which fires ~5s after startup), then every minute so the
  // banner appears promptly after the backend detects an update.
  useEffect(() => {
    if (!isAuthenticated) return;
    refreshVersion();
    const kickId = setTimeout(refreshVersion, 8000);
    const pollId = setInterval(refreshVersion, 60 * 1000);
    return () => {
      clearTimeout(kickId);
      clearInterval(pollId);
    };
  }, [isAuthenticated, refreshVersion]);

  // Poll faster while a download is in progress.
  useEffect(() => {
    if (!localDownloading) return;
    const id = setInterval(refreshVersion, 3000);
    return () => clearInterval(id);
  }, [localDownloading, refreshVersion]);

  // Reset local downloading state once the server confirms ready or an error occurred.
  useEffect(() => {
    if (versionInfo?.ready) setLocalDownloading(false);
    if (versionInfo && !versionInfo.downloading && versionInfo.error) setLocalDownloading(false);
  }, [versionInfo?.ready, versionInfo?.downloading, versionInfo?.error]);

  const handleDownload = useCallback(async () => {
    setLocalDownloading(true);
    try {
      await downloadUpdate();
      refreshVersion();
    } catch {
      setLocalDownloading(false);
    }
  }, [refreshVersion]);

  const handleRestart = useCallback(async () => {
    try {
      await restartUpdate();
    } catch { /* app is shutting down */ }
  }, []);

  const handleSkip = useCallback(async () => {
    if (!versionInfo?.latest) return;
    try {
      await skipVersion(versionInfo.latest.tag);
      refreshVersion();
    } catch { /* banner stays visible on error */ }
  }, [versionInfo?.latest, refreshVersion]);

  const navigate = useCallback((p: Page) => {
    window.location.hash = p;
    setPage(p);
  }, []);

  useEffect(() => {
    const onHash = () => setPage(readHash());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  const showBanner = isAdmin && status !== null && !status.provider_configured;

  // Auth loading state: centered spinner.
  if (authLoading) {
    return (
      <div className="flex items-center justify-center h-screen hud-grid-bg">
        <div className="text-center">
          <div className="w-6 h-6 border-2 border-orange-600 border-t-transparent rounded-full animate-spin mx-auto" />
          <p className="text-[11px] uppercase tracking-widest text-zinc-600 mt-3">Loading</p>
        </div>
      </div>
    );
  }

  // Not authenticated: show setup, login, or register.
  if (!isAuthenticated) {
    if (needsSetup === null) {
      return (
        <div className="flex items-center justify-center h-screen hud-grid-bg">
          <div className="text-center">
            <div className="w-6 h-6 border-2 border-orange-600 border-t-transparent rounded-full animate-spin mx-auto" />
            <p className="text-[11px] uppercase tracking-widest text-zinc-600 mt-3">Loading</p>
          </div>
        </div>
      );
    }
    if (needsSetup) return <SignUp />;
    if (page === 'connect') return <Connect />;
    if (page === 'register') return <Register />;
    return <Login />;
  }

  return (
    <WebSocketProvider>
    <TaskNotificationListener onNotification={handleNotification} />
    <div className="flex h-screen overflow-hidden">
      {/* Sidebar */}
      <nav className="w-56 shrink-0 border-r border-zinc-700 bg-zinc-900/50 flex flex-col">
        <div className="p-4 border-b border-zinc-700">
          <div className="flex items-center justify-between">
            <h1 className="text-xl font-semibold uppercase tracking-[0.1em] text-zinc-100 flex items-center gap-2">
              Cogitator
              <span className="text-[9px] font-bold uppercase tracking-widest px-1.5 py-0.5 rounded-full border border-orange-600/50 text-orange-500 bg-orange-950/40 leading-none">
                Beta
              </span>
            </h1>
            <NotificationBell
              unreadCount={unreadCount}
              onUnreadChange={setUnreadCount}
              refreshKey={notifRefreshKey}
              onNavigateToTasks={() => {
                window.location.hash = `chat/${encodeURIComponent('tasks:output')}`;
                setPage('chat');
              }}
            />
          </div>
          <div className="h-1 w-12 bg-orange-600 mt-2" />
        </div>
        <div className="flex-1 py-2">
          {nav.map((item) => (
            <button
              key={item.id}
              onClick={() => navigate(item.id)}
              className={`w-full flex items-center gap-3 px-4 py-2.5 text-sm font-medium uppercase tracking-widest transition-colors ${
                page === item.id
                  ? 'text-orange-500 bg-orange-900/20 border-l-2 border-orange-600'
                  : 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 border-l-2 border-transparent'
              }`}
            >
              {item.icon}
              {item.label}
            </button>
          ))}
        </div>
        <div className="p-4 border-t border-zinc-700 space-y-1">
          {user && (
            <div className="flex items-center justify-between">
              <p className="text-[12px] uppercase tracking-widest text-zinc-500 truncate" title={user.email}>
                {user.name || user.email}
              </p>
              <button
                onClick={logout}
                className="text-zinc-600 hover:text-zinc-400 transition-colors cursor-pointer"
                title="Sign out"
              >
                <LogOut size={14} />
              </button>
            </div>
          )}
          <p className="text-[12px] uppercase tracking-widest text-zinc-600">
            {versionInfo && versionInfo.current !== 'dev' ? (versionInfo.current.startsWith('v') ? versionInfo.current : `v${versionInfo.current}`) : 'Dashboard v0.1'}
          </p>
        </div>
      </nav>

      {/* Content */}
      <main className="flex-1 min-w-0 flex flex-col overflow-hidden hud-grid-bg">
        {(showBanner || (versionInfo?.latest && isNewer(versionInfo.latest.tag, versionInfo.current) &&
          bareVersion(versionInfo.latest.tag) !== bareVersion(versionInfo.skipped_version ?? ''))) && (
          <div className="shrink-0 px-6 pt-6 space-y-4">
            {showBanner && (
              <button
                onClick={() => navigate('admin')}
                className="w-full border border-orange-600/40 bg-orange-950/30 p-4 text-left cursor-pointer hover:bg-orange-950/50 transition-colors"
              >
                <p className="text-[12px] uppercase tracking-widest font-medium text-orange-500 mb-1">
                  No LLM Provider Configured
                </p>
                <p className="text-sm text-zinc-400">
                  Chat and background tasks require an LLM provider. Go to Admin to configure one.
                </p>
              </button>
            )}

            {versionInfo?.latest && isNewer(versionInfo.latest.tag, versionInfo.current) &&
              bareVersion(versionInfo.latest.tag) !== bareVersion(versionInfo.skipped_version ?? '') && (
              <div className="w-full border border-orange-600/40 bg-orange-950/30 p-4 flex items-center justify-between">
                <div>
                  <p className="text-[12px] uppercase tracking-widest font-medium text-orange-500 mb-1">
                    {versionInfo.ready ? 'Ready to Install' : 'Update Available'}: {versionInfo.latest.tag}
                  </p>
                  <p className="text-sm text-zinc-400">
                    You are running {versionInfo.current.startsWith('v') ? versionInfo.current : `v${versionInfo.current}`}.
                    {versionInfo.ready ? ' Restart to apply the update.' : ' A newer version is available.'}
                  </p>
                </div>
                <div className="flex items-center shrink-0 ml-4">
                  {versionInfo.can_auto_update ? (
                    <>
                      {versionInfo.ready ? (
                        <button
                          onClick={handleRestart}
                          className="px-4 py-2 text-sm font-medium uppercase tracking-widest bg-green-600 text-white hover:bg-green-500 transition-colors cursor-pointer"
                        >
                          Restart
                        </button>
                      ) : (
                        <button
                          onClick={handleDownload}
                          disabled={localDownloading || versionInfo.downloading}
                          className="px-4 py-2 text-sm font-medium uppercase tracking-widest bg-orange-600 text-white hover:bg-orange-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors cursor-pointer"
                        >
                          {localDownloading || versionInfo.downloading ? 'Downloading...' : 'Update Now'}
                        </button>
                      )}
                    </>
                  ) : (
                    <a
                      href={versionInfo.latest.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="px-4 py-2 text-sm font-medium uppercase tracking-widest bg-orange-600 text-white hover:bg-orange-400 transition-colors"
                    >
                      View Release
                    </a>
                  )}
                  <button
                    onClick={handleSkip}
                    className="ml-2 px-4 py-2 text-sm font-medium uppercase tracking-widest text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
                  >
                    Skip
                  </button>
                </div>
              </div>
            )}
          </div>
        )}

        <div className="flex-1 min-h-0 overflow-y-auto p-6">
          {page === 'chat' && <Chat />}
          {page === 'tasks' && <Tasks />}
          {page === 'memory' && <Memory />}
          {page === 'skills' && <Skills />}
          {page === 'connectors' && <Connectors />}
          {page === 'history' && <HistoryPage />}
          {page === 'resources' && <Resources />}
          {page === 'settings' && <SettingsPage themePreference={themePreference} setTheme={setTheme} />}
          {page === 'account' && <Account />}
          {page === 'users' && (isAdmin || isModerator) && <UsersPage />}
          {page === 'admin' && isAdmin && <Admin />}
        </div>
      </main>
    </div>
    </WebSocketProvider>
  );
}

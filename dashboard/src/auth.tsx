import { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import {
  setTokenProvider,
  setRefreshHandler,
  loginAPI,
  socialLoginAPI,
  refreshTokenAPI,
  logoutAPI,
  fetchMe,
} from './api';
import type { User } from './api';

const LS_ACCESS = 'cogitator_access_token';
const LS_REFRESH = 'cogitator_refresh_token';

export interface AuthContextValue {
  user: User | null;
  loading: boolean;
  isAuthenticated: boolean;
  isAdmin: boolean;
  isModerator: boolean;
  /** Current access token (JWT). Used by ws.tsx for WebSocket auth. */
  accessToken: string | null;
  login: (email: string, password: string) => Promise<void>;
  socialLogin: (provider: string, idToken: string, inviteCode?: string) => Promise<void>;
  loginWithTokens: (accessToken: string, refreshToken: string) => void;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

export default function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [tokenState, setTokenState] = useState<string | null>(null);

  // Ref so the token provider closure always reads the latest value.
  const accessTokenRef = useRef<string | null>(null);

  // Helper to update both the ref and the state in sync.
  const setAccessToken = useCallback((t: string | null) => {
    accessTokenRef.current = t;
    setTokenState(t);
  }, []);

  // Initialize: prefer a stored JWT (survives restarts) over the legacy
  // desktop injection. The desktop token is no longer used for API auth;
  // the server validates JWTs exclusively.
  useEffect(() => {
    const stored = localStorage.getItem(LS_ACCESS);
    if (stored) {
      setAccessToken(stored);
    }
  }, [setAccessToken]);

  // Register token provider and refresh handler with api.ts.
  // A shared in-flight promise deduplicates concurrent refresh calls, which
  // is critical because refresh tokens are single-use (rotated on each use).
  const refreshInFlight = useRef<Promise<boolean> | null>(null);

  useEffect(() => {
    setTokenProvider(() => accessTokenRef.current);
    setRefreshHandler(() => {
      if (refreshInFlight.current) return refreshInFlight.current;
      const p = (async () => {
        const rt = localStorage.getItem(LS_REFRESH);
        if (!rt) return false;
        try {
          const resp = await refreshTokenAPI(rt);
          setAccessToken(resp.access_token);
          localStorage.setItem(LS_ACCESS, resp.access_token);
          localStorage.setItem(LS_REFRESH, resp.refresh_token);
          return true;
        } catch {
          setAccessToken(null);
          localStorage.removeItem(LS_ACCESS);
          localStorage.removeItem(LS_REFRESH);
          setUser(null);
          return false;
        } finally {
          refreshInFlight.current = null;
        }
      })();
      refreshInFlight.current = p;
      return p;
    });
  }, [setAccessToken]);

  // Hydrate user on mount. If no stored JWT exists but desktop credentials
  // are injected (macOS app), auto-login to get a JWT seamlessly.
  useEffect(() => {
    let cancelled = false;
    // During a page reload (Cmd-R), React may not unmount properly, so the
    // cleanup function below may not fire. Listen for pagehide to ensure we
    // stop mutating state on the dying page (prevents login-screen flash).
    const markCancelled = () => { cancelled = true; };
    window.addEventListener('pagehide', markCancelled);
    (async () => {
      // Case 1: We have a stored JWT. Try to use it.
      if (accessTokenRef.current) {
        try {
          const me = await fetchMe();
          if (!cancelled) setUser(me);
          if (!cancelled) setLoading(false);
          return;
        } catch {
          // Token invalid/expired and refresh also failed. Reset the in-memory
          // ref so we fall through to Case 2, but do NOT clear localStorage.
          // Stale tokens are harmless: they get overwritten on next successful
          // login/refresh and expire naturally.
          accessTokenRef.current = null;
        }
      }

      if (!cancelled) setLoading(false);
    })();
    return () => {
      cancelled = true;
      window.removeEventListener('pagehide', markCancelled);
    };
  }, [setAccessToken]);

  const login = useCallback(async (email: string, password: string) => {
    const resp = await loginAPI(email, password);
    setAccessToken(resp.access_token);
    localStorage.setItem(LS_ACCESS, resp.access_token);
    localStorage.setItem(LS_REFRESH, resp.refresh_token);
    if (resp.user) {
      setUser(resp.user);
    } else {
      setUser(await fetchMe());
    }
  }, [setAccessToken]);

  const socialLogin = useCallback(async (provider: string, idToken: string, inviteCode?: string) => {
    const resp = await socialLoginAPI(provider, idToken, inviteCode);
    setAccessToken(resp.access_token);
    localStorage.setItem(LS_ACCESS, resp.access_token);
    localStorage.setItem(LS_REFRESH, resp.refresh_token);
    if (resp.user) {
      setUser(resp.user);
    } else {
      setUser(await fetchMe());
    }
  }, [setAccessToken]);

  const loginWithTokens = useCallback((at: string, rt: string) => {
    setAccessToken(at);
    localStorage.setItem(LS_ACCESS, at);
    localStorage.setItem(LS_REFRESH, rt);
    // Eagerly load user profile in the background.
    fetchMe().then(setUser).catch(() => {});
  }, [setAccessToken]);

  const logout = useCallback(async () => {
    try { await logoutAPI(); } catch { /* best effort */ }
    setAccessToken(null);
    localStorage.removeItem(LS_ACCESS);
    localStorage.removeItem(LS_REFRESH);
    setUser(null);
  }, [setAccessToken]);

  const isAuthenticated = user !== null;
  const isAdmin = user?.role === 'admin';
  const isModerator = user?.role === 'moderator' || isAdmin;

  return (
    <AuthContext.Provider value={{
      user, loading, isAuthenticated, isAdmin, isModerator,
      accessToken: tokenState,
      login, socialLogin, loginWithTokens, logout,
    }}>
      {children}
    </AuthContext.Provider>
  );
}

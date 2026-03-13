import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '../auth';
import { fetchAuthProviders, fetchVersionInfo, isWebBrowser, getServerUrl } from '../api';
import type { AuthProviders } from '../api';
// TODO: re-enable when Apple Sign-In is fully supported in the desktop app
// import { getAppleIdToken } from '../social-sdk';
import StripedButton from '../components/StripedButton';

const LS_PENDING_CLAIM = 'cogitator_pending_claim';

export default function Login() {
  const { login, loginWithTokens } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [socialLoading, setSocialLoading] = useState<'google' | 'apple' | null>(null);
  const [providers, setProviders] = useState<AuthProviders | null>(null);
  const [version, setVersion] = useState('');

  useEffect(() => {
    fetchAuthProviders().then(setProviders).catch(() => {});
    fetchVersionInfo().then((v) => { if (v.current && v.current !== 'dev') setVersion(v.current); }).catch(() => {});
  }, []);

  // Try to claim pending Google OAuth tokens from localStorage.
  const tryClaim = useCallback(() => {
    const claimID = localStorage.getItem(LS_PENDING_CLAIM);
    if (!claimID) return;

    localStorage.removeItem(LS_PENDING_CLAIM);
    setSocialLoading('google');

    fetch(`${getServerUrl()}/api/auth/claim/${claimID}`)
      .then(async (res) => {
        if (res.status === 202) {
          // Not ready yet; put it back and try again on next focus.
          localStorage.setItem(LS_PENDING_CLAIM, claimID);
          setSocialLoading(null);
          return;
        }
        if (!res.ok) {
          const body = await res.json().catch(() => ({ error: 'Sign-in failed' }));
          setError(body.error || 'Sign-in failed');
          setSocialLoading(null);
          return;
        }
        const tokens = await res.json();
        loginWithTokens(tokens.access_token, tokens.refresh_token);
      })
      .catch(() => {
        setSocialLoading(null);
        setError('Sign-in failed');
      });
  }, [loginWithTokens]);

  // Check on mount and whenever the window regains focus (bfcache/tab switch).
  useEffect(() => {
    tryClaim();
    const onVisible = () => { if (document.visibilityState === 'visible') tryClaim(); };
    const onPageShow = (e: PageTransitionEvent) => { if (e.persisted) tryClaim(); };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('pageshow', onPageShow);
    window.addEventListener('focus', tryClaim);
    return () => {
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('pageshow', onPageShow);
      window.removeEventListener('focus', tryClaim);
    };
  }, [tryClaim]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await login(email, password);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setSubmitting(false);
    }
  };

  // TODO: re-enable when Apple Sign-In is fully supported in the desktop app
  // const handleSocialError = useCallback((err: unknown) => {
  //   if (err instanceof Error && err.message.includes('403')) {
  //     window.location.hash = '#register';
  //     return;
  //   }
  //   setError(err instanceof Error ? err.message : 'Social sign-in failed');
  // }, []);

  const handleGoogleSignIn = useCallback(() => {
    const claimID = crypto.randomUUID();
    localStorage.setItem(LS_PENDING_CLAIM, claimID);
    const src = isWebBrowser ? '&source=web' : '';
    window.location.href = `${getServerUrl()}/api/auth/google/start?return_to=login&purpose=login&claim_id=${claimID}${src}`;
  }, []);

  // TODO: re-enable when Apple Sign-In is fully supported in the desktop app
  // const handleAppleSignIn = useCallback(async () => {
  //   setError('');
  //   setSocialLoading('apple');
  //   try {
  //     const idToken = await getAppleIdToken();
  //     await socialLogin('apple', idToken);
  //   } catch (err) {
  //     handleSocialError(err);
  //   } finally {
  //     setSocialLoading(null);
  //   }
  // }, [socialLogin, handleSocialError]);

  const busy = submitting || socialLoading !== null;

  return (
    <div className="flex items-center justify-center min-h-screen hud-grid-bg">
      <form onSubmit={handleSubmit} className="w-full max-w-sm space-y-6">
        {/* Branding */}
        <div className="text-center">
          <h1 className="text-3xl font-semibold uppercase tracking-[0.1em] text-zinc-100">
            Cogitator
          </h1>
          <div className="h-1 w-12 bg-orange-600 mt-2 mx-auto" />
          <p className="text-[12px] uppercase tracking-widest text-zinc-500 mt-3">Sign in to continue</p>
        </div>

        {error && (
          <div className="border border-red-500/40 bg-red-950/20 p-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {/* Social sign-in */}
        {(providers?.google || providers?.apple) && (
          <>
            <div className="space-y-3">
              {providers.google && (
                <button
                  onClick={handleGoogleSignIn}
                  type="button"
                  disabled={busy}
                  className="w-full flex items-center justify-center gap-3 bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm hover:border-zinc-500 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <svg width="16" height="16" viewBox="0 0 48 48" aria-hidden="true">
                    <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/>
                    <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/>
                    <path fill="#FBBC05" d="M10.53 28.59a14.5 14.5 0 0 1 0-9.18l-7.98-6.19a24.0 24.0 0 0 0 0 21.56l7.98-6.19z"/>
                    <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/>
                  </svg>
                  {socialLoading === 'google' ? 'Signing in...' : 'Sign in with Google'}
                </button>
              )}

              {/* TODO: re-enable when Apple Sign-In is fully supported in the desktop app
              {providers.apple && (
                <button
                  onClick={handleAppleSignIn}
                  type="button"
                  disabled={busy}
                  className="w-full flex items-center justify-center gap-3 bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm hover:border-zinc-500 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <svg width="16" height="16" viewBox="0 0 17 20" fill="currentColor" aria-hidden="true">
                    <path d="M13.545 10.239c-.022-2.234 1.823-3.306 1.906-3.358-.037-.053-1.497-2.21-3.822-2.21-1.628 0-2.96.978-3.745.978-.831 0-2.002-.953-3.292-.927C2.774 4.749 1.126 5.8.477 7.472c-1.334 3.438.342 8.535 2.384 11.326.632.91 1.38 1.928 2.363 1.892.95-.038 1.31-.613 2.458-.613 1.147 0 1.47.613 2.467.594.989-.019 1.629-.927 2.253-1.84.717-1.044 1.01-2.066 1.025-2.118-.022-.01-1.965-.755-1.984-2.995l.002-.001zM11.703 3.04C12.23 2.4 12.59 1.519 12.49.62c-.756.031-1.674.504-2.216 1.14-.485.563-.91 1.462-.796 2.325.844.066 1.706-.429 2.225-1.045z"/>
                  </svg>
                  {socialLoading === 'apple' ? 'Signing in...' : 'Sign in with Apple'}
                </button>
              )}
              */}
            </div>

            {/* Divider */}
            <div className="flex items-center gap-3">
              <div className="flex-1 border-t border-zinc-800" />
              <span className="text-[11px] uppercase tracking-widest text-zinc-600">or sign in with password</span>
              <div className="flex-1 border-t border-zinc-800" />
            </div>
          </>
        )}

        <div className="space-y-4">
          <div>
            <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
              Email
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
              autoFocus
              required
              className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors"
            />
          </div>

          <div>
            <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
              Password
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
              className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors"
            />
          </div>
        </div>

        <StripedButton type="submit" disabled={busy} className="w-full">
          {submitting ? 'Signing in...' : 'Sign In'}
        </StripedButton>

        <p className="text-center text-sm text-zinc-600">
          Have an invite code?{' '}
          <a
            href="#register"
            className="text-orange-600 hover:text-orange-500 transition-colors"
          >
            Create an account
          </a>
        </p>

        <p className="text-center text-sm text-zinc-600">
          <a
            href="#connect"
            className="text-zinc-500 hover:text-orange-500 transition-colors"
          >
            Connect to a different server
          </a>
        </p>

        {version && (
          <p className="text-center text-[12px] uppercase tracking-widest text-zinc-700 mt-4">
            {version.startsWith('v') ? version : `v${version}`}
          </p>
        )}
      </form>
    </div>
  );
}

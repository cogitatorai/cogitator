import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '../auth';
import { registerAPI, fetchAuthProviders } from '../api';
import type { AuthProviders } from '../api';
// TODO: re-enable when Apple Sign-In is fully supported in the desktop app
// import { getAppleIdToken } from '../social-sdk';
import StripedButton from '../components/StripedButton';

const LS_PENDING_CLAIM = 'cogitator_pending_claim';

function getHashParams(): URLSearchParams {
  const hash = window.location.hash;
  const qIdx = hash.indexOf('?');
  if (qIdx === -1) return new URLSearchParams();
  return new URLSearchParams(hash.slice(qIdx));
}

export default function Register() {
  const { loginWithTokens } = useAuth();
  const [inviteCode, setInviteCode] = useState(() => getHashParams().get('code') ?? '');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [socialLoading, setSocialLoading] = useState<'google' | 'apple' | null>(null);
  const [providers, setProviders] = useState<AuthProviders | null>(null);

  useEffect(() => {
    fetchAuthProviders().then(setProviders).catch(() => {});
  }, []);

  // Try to claim pending Google OAuth tokens from localStorage.
  const tryClaim = useCallback(() => {
    const claimID = localStorage.getItem(LS_PENDING_CLAIM);
    if (!claimID) return;

    localStorage.removeItem(LS_PENDING_CLAIM);
    setSocialLoading('google');

    fetch(`/api/auth/claim/${claimID}`)
      .then(async (res) => {
        if (res.status === 202) {
          localStorage.setItem(LS_PENDING_CLAIM, claimID);
          setSocialLoading(null);
          return;
        }
        if (!res.ok) {
          const body = await res.json().catch(() => ({ error: 'Sign-up failed' }));
          setError(body.error || 'Sign-up failed');
          setSocialLoading(null);
          return;
        }
        const tokens = await res.json();
        loginWithTokens(tokens.access_token, tokens.refresh_token);
        window.location.hash = 'chat';
      })
      .catch(() => {
        setSocialLoading(null);
        setError('Sign-up failed');
      });
  }, [loginWithTokens]);

  // Check on mount and whenever the window regains focus.
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

    if (password !== confirmPassword) {
      setError('Passwords do not match');
      return;
    }
    if (password.length < 6) {
      setError('Password must be at least 6 characters');
      return;
    }
    setSubmitting(true);
    try {
      const resp = await registerAPI(email, name, password, inviteCode);
      loginWithTokens(resp.access_token, resp.refresh_token);
      window.location.hash = 'chat';
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Registration failed');
    } finally {
      setSubmitting(false);
    }
  };

  // TODO: re-enable when Apple Sign-In is fully supported in the desktop app
  // const handleSocialError = useCallback((err: unknown) => {
  //   setError(err instanceof Error ? err.message : 'Social sign-in failed');
  // }, []);

  const handleGoogleSignIn = useCallback(() => {
    const claimID = crypto.randomUUID();
    localStorage.setItem(LS_PENDING_CLAIM, claimID);
    const ic = encodeURIComponent(inviteCode);
    window.location.href = `/api/auth/google/start?return_to=register&purpose=login&claim_id=${claimID}&invite_code=${ic}`;
  }, [inviteCode]);

  // TODO: re-enable when Apple Sign-In is fully supported in the desktop app
  // const handleAppleSignIn = useCallback(async () => {
  //   setError('');
  //   setSocialLoading('apple');
  //   try {
  //     const idToken = await getAppleIdToken();
  //     await socialLogin('apple', idToken, inviteCode);
  //     window.location.hash = 'chat';
  //   } catch (err) {
  //     handleSocialError(err);
  //   } finally {
  //     setSocialLoading(null);
  //   }
  // }, [inviteCode, socialLogin, handleSocialError]);

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
          <p className="text-[12px] uppercase tracking-widest text-zinc-500 mt-3">Create your account</p>
        </div>

        {error && (
          <div className="border border-red-500/40 bg-red-950/20 p-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {/* Invite code (always visible, above social buttons) */}
        <div>
          <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
            Invite Code
          </label>
          <input
            type="text"
            value={inviteCode}
            onChange={(e) => setInviteCode(e.target.value)}
            required
            placeholder="Paste your invite code"
            autoFocus={!inviteCode}
            className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors placeholder:text-zinc-600"
          />
        </div>

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
                  {socialLoading === 'google' ? 'Signing up...' : 'Sign up with Google'}
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
                  {socialLoading === 'apple' ? 'Signing up...' : 'Sign up with Apple'}
                </button>
              )}
              */}
            </div>

            {/* Divider */}
            <div className="flex items-center gap-3">
              <div className="flex-1 border-t border-zinc-800" />
              <span className="text-[11px] uppercase tracking-widest text-zinc-600">or register with password</span>
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
              autoFocus={!!inviteCode}
              required
              placeholder="you@example.com"
              className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors placeholder:text-zinc-600"
            />
          </div>

          <div>
            <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
              Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Optional"
              className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors placeholder:text-zinc-600"
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
              autoComplete="new-password"
              required
              className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors"
            />
          </div>

          <div>
            <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
              Confirm Password
            </label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              autoComplete="new-password"
              required
              className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors"
            />
          </div>
        </div>

        <StripedButton type="submit" disabled={busy} className="w-full">
          {submitting ? 'Creating account...' : 'Create Account'}
        </StripedButton>

        <div className="text-center">
          <a
            href="#login"
            className="text-[12px] uppercase tracking-widest text-zinc-500 hover:text-orange-500 transition-colors"
          >
            Already have an account? Sign in
          </a>
        </div>
      </form>
    </div>
  );
}

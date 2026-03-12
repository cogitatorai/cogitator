import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '../auth';
import { setupAPI, fetchAuthProviders } from '../api';
import type { AuthProviders } from '../api';
import StripedButton from '../components/StripedButton';

const LS_PENDING_CLAIM = 'cogitator_pending_claim';

export default function SignUp() {
  const { loginWithTokens } = useAuth();
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
      const resp = await setupAPI(email, name, password);
      loginWithTokens(resp.access_token, resp.refresh_token);
      window.location.hash = 'chat';
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed');
    } finally {
      setSubmitting(false);
    }
  };

  const handleGoogleSignIn = useCallback(() => {
    const claimID = crypto.randomUUID();
    localStorage.setItem(LS_PENDING_CLAIM, claimID);
    window.location.href = `/api/auth/google/start?return_to=signup&purpose=login&claim_id=${claimID}`;
  }, []);

  const busy = submitting || socialLoading !== null;

  return (
    <div className="flex items-center justify-center min-h-screen hud-grid-bg">
      <form onSubmit={handleSubmit} className="w-full max-w-sm space-y-6">
        <div className="text-center">
          <h1 className="text-3xl font-semibold uppercase tracking-[0.1em] text-zinc-100">
            Cogitator
          </h1>
          <div className="h-1 w-12 bg-orange-600 mt-2 mx-auto" />
          <p className="text-[12px] uppercase tracking-widest text-zinc-500 mt-3">Create your admin account</p>
        </div>

        {error && (
          <div className="border border-red-500/40 bg-red-950/20 p-3 text-sm text-red-400">
            {error}
          </div>
        )}

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
                  {socialLoading === 'google' ? 'Setting up...' : 'Continue with Google'}
                </button>
              )}
            </div>

            <div className="flex items-center gap-3">
              <div className="flex-1 border-t border-zinc-800" />
              <span className="text-[11px] uppercase tracking-widest text-zinc-600">or set up with password</span>
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
          {submitting ? 'Setting up...' : 'Create Account'}
        </StripedButton>

        <p className="text-center text-sm text-zinc-600">
          Already have an account?{' '}
          <a
            href="#login"
            className="text-orange-600 hover:text-orange-500 transition-colors"
          >
            Sign in
          </a>
        </p>
      </form>
    </div>
  );
}

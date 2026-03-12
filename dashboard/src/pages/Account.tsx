import { useState, useEffect, useCallback } from 'react';
import { updateMe, listOAuthLinks, linkOAuth, unlinkOAuth, fetchAuthProviders, isWebBrowser } from '../api';
import type { OAuthLink, AuthProviders } from '../api';
import { useAuth } from '../auth';
import { getAppleIdToken } from '../social-sdk';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import StripedButton from '../components/StripedButton';

const GoogleIcon = () => (
  <svg width="16" height="16" viewBox="0 0 48 48" aria-hidden="true">
    <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/>
    <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/>
    <path fill="#FBBC05" d="M10.53 28.59a14.5 14.5 0 0 1 0-9.18l-7.98-6.19a24.0 24.0 0 0 0 0 21.56l7.98-6.19z"/>
    <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/>
  </svg>
);

const AppleIcon = () => (
  <svg width="16" height="16" viewBox="0 0 17 20" fill="currentColor" aria-hidden="true">
    <path d="M13.545 10.239c-.022-2.234 1.823-3.306 1.906-3.358-.037-.053-1.497-2.21-3.822-2.21-1.628 0-2.96.978-3.745.978-.831 0-2.002-.953-3.292-.927C2.774 4.749 1.126 5.8.477 7.472c-1.334 3.438.342 8.535 2.384 11.326.632.91 1.38 1.928 2.363 1.892.95-.038 1.31-.613 2.458-.613 1.147 0 1.47.613 2.467.594.989-.019 1.629-.927 2.253-1.84.717-1.044 1.01-2.066 1.025-2.118-.022-.01-1.965-.755-1.984-2.995l.002-.001zM11.703 3.04C12.23 2.4 12.59 1.519 12.49.62c-.756.031-1.674.504-2.216 1.14-.485.563-.91 1.462-.796 2.325.844.066 1.706-.429 2.225-1.045z"/>
  </svg>
);

export default function Account() {
  const { user, accessToken } = useAuth();
  const [currentPassword, setCurrentPassword] = useState('');
  const [name, setName] = useState(user?.name ?? '');
  const [email, setEmail] = useState(user?.email ?? '');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const [links, setLinks] = useState<OAuthLink[]>([]);
  const [linkLoading, setLinkLoading] = useState<'google' | 'apple' | null>(null);
  const [unlinkLoading, setUnlinkLoading] = useState<string | null>(null);
  const [linkError, setLinkError] = useState<string | null>(null);
  const [providers, setProviders] = useState<AuthProviders | null>(null);

  useEffect(() => {
    if (user) {
      setName(user.name);
      setEmail(user.email);
    }
  }, [user]);

  useEffect(() => {
    fetchAuthProviders().then(setProviders).catch(() => {});
  }, []);

  const refreshLinks = useCallback(() => {
    listOAuthLinks().then(setLinks).catch(() => {});
  }, []);

  useEffect(() => { refreshLinks(); }, [refreshLinks]);

  useEffect(() => {
    const onFocus = () => refreshLinks();
    const onVisible = () => { if (document.visibilityState === 'visible') refreshLinks(); };
    window.addEventListener('focus', onFocus);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.removeEventListener('focus', onFocus);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [refreshLinks]);

  // TODO: re-enable 'apple' when Apple Sign-In is fully supported in the desktop app
  const ALL_PROVIDERS = ['google'] as const;
  const linkedProviders = new Set(links.map((l) => l.provider));

  const handleConnectGoogle = useCallback(() => {
    const token = encodeURIComponent(accessToken ?? '');
    const src = isWebBrowser ? '&source=web' : '';
    window.location.href = `/api/auth/google/start?return_to=account&purpose=link&token=${token}${src}`;
  }, [accessToken]);

  const handleConnectApple = useCallback(async () => {
    setLinkError(null);
    setLinkLoading('apple');
    try {
      const idToken = await getAppleIdToken();
      await linkOAuth('apple', idToken);
      refreshLinks();
    } catch (err) {
      setLinkError(err instanceof Error ? err.message : 'Failed to link Apple');
    } finally {
      setLinkLoading(null);
    }
  }, [refreshLinks]);

  const handleDisconnect = useCallback(async (provider: string) => {
    setLinkError(null);
    setUnlinkLoading(provider);
    try {
      await unlinkOAuth(provider);
      refreshLinks();
    } catch (err) {
      setLinkError(err instanceof Error ? err.message : 'Failed to disconnect provider');
    } finally {
      setUnlinkLoading(null);
    }
  }, [refreshLinks]);

  const providerLabel = (p: string) => p === 'google' ? 'Google' : p === 'apple' ? 'Apple' : p;
  const providerIcon = (p: string) => p === 'google' ? <GoogleIcon /> : p === 'apple' ? <AppleIcon /> : null;

  const nameChanged = name !== (user?.name ?? '');
  const emailChanged = email !== (user?.email ?? '');
  const passwordChanged = newPassword.length > 0;
  const hasChanges = currentPassword.length > 0 && (nameChanged || emailChanged || passwordChanged);

  const handleSave = async () => {
    setError(null);
    setSuccess(false);

    if (!currentPassword) {
      setError('Current password is required to save changes');
      return;
    }
    if (passwordChanged) {
      if (newPassword.length < 6) {
        setError('New password must be at least 6 characters');
        return;
      }
      if (newPassword !== confirmPassword) {
        setError('New passwords do not match');
        return;
      }
    }

    setSaving(true);
    try {
      const body: { current_password: string; name?: string; email?: string; password?: string } = {
        current_password: currentPassword,
      };
      if (nameChanged) body.name = name;
      if (emailChanged) body.email = email;
      if (passwordChanged) body.password = newPassword;

      const updated = await updateMe(body);
      setName(updated.name);
      setEmail(updated.email);
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to update account';
      // Extract message from API error format "API 4xx: ..."
      const match = msg.match(/API \d+: (.+)/);
      setError(match ? match[1] : msg);
    } finally {
      setSaving(false);
    }
  };

  const hasPassword = user?.has_password !== false;

  return (
    <div>
      <PageHeader title="Account" subtitle="Manage your profile and credentials" />

      <div className="space-y-6">
        <Panel>
          <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
            Profile
          </h3>
          <p className="text-sm text-zinc-600 mb-4">
            Update your name, email, or password.
          </p>

          {error && (
            <div className="text-sm text-red-500 mb-3 p-2 border border-red-500/20">{error}</div>
          )}
          {success && (
            <div className="text-sm text-green-500 mb-3 p-2 border border-green-500/20">Account updated.</div>
          )}

          {hasPassword ? (
            <>
              <div className="space-y-4">
                <div className="max-w-sm">
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">Current Password</label>
                  <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)}
                    placeholder="Required to save changes"
                    className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600" />
                </div>

                <div className="border-t border-zinc-800 my-2" />

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">Name</label>
                    <input type="text" value={name} onChange={(e) => setName(e.target.value)}
                      className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none" />
                  </div>
                  <div>
                    <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">Email</label>
                    <input type="email" value={email} onChange={(e) => setEmail(e.target.value)}
                      className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none" />
                  </div>
                </div>

                <div className="border-t border-zinc-800 my-2" />

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">New Password</label>
                    <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)}
                      placeholder="Leave blank to keep current"
                      className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600" />
                  </div>
                  <div>
                    <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">Confirm Password</label>
                    <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)}
                      placeholder="Repeat new password"
                      className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600" />
                  </div>
                </div>
              </div>

              <div className="mt-4 flex justify-end">
                <StripedButton onClick={handleSave} disabled={saving || !hasChanges}>
                  {saving ? 'Saving...' : 'Save Changes'}
                </StripedButton>
              </div>
            </>
          ) : (
            <p className="text-sm text-zinc-500">
              Your account uses social sign-in only. Connect a password via your linked provider to edit profile details here.
            </p>
          )}
        </Panel>

        <Panel>
          <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
            Connected Accounts
          </h3>
          <p className="text-sm text-zinc-600 mb-4">
            Link social providers for faster sign-in.
          </p>

          {linkError && (
            <div className="text-sm text-red-500 mb-3 p-2 border border-red-500/20">{linkError}</div>
          )}

          <div className="space-y-3">
            {ALL_PROVIDERS.map((provider) => {
              const link = links.find((l) => l.provider === provider);
              const isLinked = linkedProviders.has(provider);
              const configured = provider === 'google' ? providers?.google : providers?.apple;
              if (!configured && !isLinked) return null;

              return (
                <div key={provider} className="flex items-center justify-between p-3 border border-zinc-700 bg-zinc-900">
                  <div className="flex items-center gap-3">
                    {providerIcon(provider)}
                    <div>
                      <span className="text-base text-zinc-300 font-medium">{providerLabel(provider)}</span>
                      {link && (
                        <p className="text-xs text-zinc-500">{link.email}</p>
                      )}
                    </div>
                  </div>
                  {isLinked ? (
                    <button
                      onClick={() => handleDisconnect(provider)}
                      disabled={unlinkLoading === provider}
                      className="text-xs text-zinc-500 hover:text-red-400 border border-zinc-700 hover:border-red-500/40 px-3 py-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      {unlinkLoading === provider ? 'Disconnecting...' : 'Disconnect'}
                    </button>
                  ) : (
                    <StripedButton
                      onClick={provider === 'google' ? handleConnectGoogle : handleConnectApple}
                      disabled={linkLoading === provider}
                    >
                      {linkLoading === provider ? 'Connecting...' : 'Connect'}
                    </StripedButton>
                  )}
                </div>
              );
            })}
          </div>
        </Panel>
      </div>
    </div>
  );
}

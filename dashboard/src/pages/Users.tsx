import { useState, useEffect, useCallback } from 'react';
import { Trash2, Copy, Check, KeyRound } from 'lucide-react';
import { useAuth } from '../auth';
import {
  usePolling,
  fetchJSON,
  putJSON,
  fetchUsers,
  updateUserRole,
  resetUserPassword,
  deleteUser,
  createInviteCode,
  fetchInviteCodes,
  deleteInviteCode,
} from '../api';
import type { InviteCode, UserRole, Settings } from '../api';
import PageHeader from '../components/PageHeader';
import Panel from '../components/Panel';
import StripedButton from '../components/StripedButton';

function SectionHeader({ title }: { title: string }) {
  return (
    <div className="flex items-center gap-3 pt-2">
      <h2 className="text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 whitespace-nowrap">
        {title}
      </h2>
      <div className="flex-1 h-px bg-zinc-700" />
    </div>
  );
}

function RoleBadge({ role }: { role: UserRole }) {
  const styles: Record<UserRole, string> = {
    admin: 'text-orange-500 border-orange-500/40 bg-orange-900/10',
    moderator: 'text-blue-400 border-blue-400/40 bg-blue-900/10',
    user: 'text-zinc-400 border-zinc-500/40 bg-zinc-800/30',
  };
  return (
    <span className={`inline-block text-[13px] uppercase tracking-[0.2em] font-medium px-2 py-1 border ${styles[role]}`}>
      {role}
    </span>
  );
}

function formatDate(iso: string): string {
  if (!iso) return '';
  return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

const inputClass = 'bg-zinc-900 border border-zinc-700 text-zinc-300 text-sm px-2.5 h-[38px] focus:border-orange-600 focus:outline-none';

// Users section

function UsersSection() {
  const { user: currentUser } = useAuth();
  const { data, refresh } = usePolling(() => fetchUsers(), 15000);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [pendingPassword, setPendingPassword] = useState<string | null>(null);
  const [newPassword, setNewPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [errorMsg, setErrorMsg] = useState('');

  const users = data?.users ?? [];

  const handleRoleChange = useCallback(async (id: string, role: UserRole) => {
    setBusy(true);
    setErrorMsg('');
    try {
      await updateUserRole(id, role);
      refresh();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : 'Failed to update role');
    }
    setBusy(false);
  }, [refresh]);

  const handleDelete = useCallback(async (id: string) => {
    setBusy(true);
    setErrorMsg('');
    try {
      await deleteUser(id);
      setPendingDelete(null);
      refresh();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : 'Failed to delete user');
    }
    setBusy(false);
  }, [refresh]);

  const handlePasswordReset = useCallback(async (id: string) => {
    if (!newPassword) return;
    setBusy(true);
    setErrorMsg('');
    try {
      await resetUserPassword(id, newPassword);
      setPendingPassword(null);
      setNewPassword('');
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : 'Failed to reset password');
    }
    setBusy(false);
  }, [newPassword]);

  return (
    <>
      <SectionHeader title="Users" />
      {errorMsg && (
        <div className="border border-red-500/40 bg-red-950/30 px-4 py-2 text-sm text-red-400 flex items-center justify-between">
          <span>{errorMsg}</span>
          <button onClick={() => setErrorMsg('')} className="text-red-500 hover:text-red-300 ml-4 cursor-pointer text-xs uppercase tracking-widest">Dismiss</button>
        </div>
      )}
      <Panel>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-[11px] uppercase tracking-[0.15em] text-zinc-500 border-b border-zinc-700">
                <th className="text-left py-2 pr-4 font-semibold">Email</th>
                <th className="text-left py-2 pr-4 font-semibold">Name</th>
                <th className="text-left py-2 pr-4 font-semibold">Role</th>
                <th className="text-left py-2 pr-4 font-semibold">Created</th>
                <th className="text-right py-2 font-semibold">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => {
                const isSelf = u.id === currentUser?.id;
                return (
                  <tr
                    key={u.id}
                    className={`border-b border-zinc-800 ${isSelf ? 'bg-orange-950/10' : ''}`}
                  >
                    <td className="py-2.5 pr-4 text-zinc-400">{u.email}</td>
                    <td className="py-2.5 pr-4 text-zinc-400">{u.name || '\u2014'}</td>
                    <td className="py-2.5 pr-4"><RoleBadge role={u.role} /></td>
                    <td className="py-2.5 pr-4 text-zinc-500">{formatDate(u.created_at)}</td>
                    <td className="py-2.5 text-right">
                      {pendingPassword === u.id ? (
                        <div className="flex items-center justify-end gap-2">
                          <input
                            type="password"
                            value={newPassword}
                            onChange={(e) => setNewPassword(e.target.value)}
                            placeholder="New password"
                            className="bg-zinc-900 border border-zinc-700 text-zinc-300 text-xs px-2 py-1 w-32 focus:border-orange-600 focus:outline-none"
                            autoFocus
                          />
                          <button
                            onClick={() => handlePasswordReset(u.id)}
                            disabled={busy || !newPassword}
                            className="text-orange-500 hover:text-orange-400 text-[11px] uppercase tracking-widest cursor-pointer disabled:opacity-50"
                          >
                            Save
                          </button>
                          <button
                            onClick={() => { setPendingPassword(null); setNewPassword(''); }}
                            className="text-zinc-500 hover:text-zinc-300 text-[11px] uppercase tracking-widest cursor-pointer"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : pendingDelete === u.id ? (
                        <div className="flex items-center justify-end gap-1">
                          <button
                            onClick={() => handleDelete(u.id)}
                            disabled={busy}
                            className="text-red-500 hover:text-red-400 text-[11px] uppercase tracking-widest cursor-pointer disabled:opacity-50"
                          >
                            Confirm
                          </button>
                          <button
                            onClick={() => setPendingDelete(null)}
                            className="text-zinc-500 hover:text-zinc-300 text-[11px] uppercase tracking-widest cursor-pointer"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : isSelf ? (
                        <div className="flex items-center justify-end gap-2">
                          <button
                            onClick={() => setPendingPassword(u.id)}
                            className="text-zinc-600 hover:text-orange-500 transition-colors cursor-pointer"
                            title="Change password"
                          >
                            <KeyRound size={14} />
                          </button>
                          <span className="text-[11px] uppercase tracking-widest text-zinc-600">you</span>
                        </div>
                      ) : (
                        <div className="flex items-center justify-end gap-2">
                          <select
                            value={u.role}
                            disabled={busy}
                            onChange={(e) => handleRoleChange(u.id, e.target.value as UserRole)}
                            className="bg-zinc-900 border border-zinc-700 text-zinc-300 text-xs px-2 py-1 focus:border-orange-600 focus:outline-none cursor-pointer"
                          >
                            <option value="admin">Admin</option>
                            <option value="moderator">Moderator</option>
                            <option value="user">User</option>
                          </select>

                          <button
                            onClick={() => setPendingPassword(u.id)}
                            className="text-zinc-600 hover:text-orange-500 transition-colors cursor-pointer"
                            title="Reset password"
                          >
                            <KeyRound size={14} />
                          </button>

                          <button
                            onClick={() => setPendingDelete(u.id)}
                            className="text-zinc-600 hover:text-red-500 transition-colors cursor-pointer"
                            title="Delete user"
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
              {users.length === 0 && (
                <tr>
                  <td colSpan={5} className="py-6 text-center text-zinc-600 text-sm">
                    No users found
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}

// Invite codes section

function composeMobileCode(code: string, publicUrl?: string): string {
  const base = publicUrl || window.location.origin;
  return btoa(`${base}|${code}`);
}

function InviteCodesSection({ publicUrl }: { publicUrl: string }) {
  const { data, refresh } = usePolling(() => fetchInviteCodes(), 15000);
  const [role, setRole] = useState<UserRole>('user');
  const [expiresAt, setExpiresAt] = useState('');
  const [creating, setCreating] = useState(false);
  const [newCode, setNewCode] = useState<InviteCode | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const codes = data?.codes ?? [];

  const handleCreate = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    try {
      const code = await createInviteCode(role, expiresAt || undefined);
      setNewCode(code);
      setExpiresAt('');
      refresh();
    } catch { /* ignore */ }
    setCreating(false);
  }, [role, expiresAt, refresh]);

  const handleCopy = useCallback(async (text: string, key: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(null), 2000);
  }, []);

  const handleDeleteCode = useCallback(async (code: string) => {
    try {
      await deleteInviteCode(code);
      if (newCode?.code === code) setNewCode(null);
      refresh();
    } catch { /* ignore */ }
  }, [refresh, newCode]);

  const codeStatus = (c: InviteCode): { label: string; style: string } => {
    if (c.redeemed_by) return { label: 'Redeemed', style: 'text-zinc-500' };
    if (c.expires_at && new Date(c.expires_at) < new Date()) return { label: 'Expired', style: 'text-red-500' };
    return { label: 'Available', style: 'text-green-500' };
  };

  return (
    <>
      <SectionHeader title="Invite Codes" />

      <Panel>
        <form onSubmit={handleCreate} className="flex items-end gap-3 flex-wrap">
          <div>
            <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
              Role
            </label>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as UserRole)}
              className={`${inputClass} cursor-pointer`}
            >
              <option value="user">User</option>
              <option value="moderator">Moderator</option>
              <option value="admin">Admin</option>
            </select>
          </div>
          <div>
            <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
              Expires (optional)
            </label>
            <input
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
              className={inputClass}
            />
          </div>
          <StripedButton type="submit" disabled={creating} className="h-[38px]">
            {creating ? 'Generating...' : 'Generate'}
          </StripedButton>
        </form>
      </Panel>

      {newCode && (
        <Panel className="hud-panel-orange">
          <div className="flex items-center justify-between gap-3">
            <div className="min-w-0 flex-1">
              <p className="text-[11px] uppercase tracking-[0.15em] font-semibold text-orange-500 mb-1">
                Mobile Invite Code
              </p>
              <code className="text-sm font-mono text-zinc-100 select-all break-all">{composeMobileCode(newCode.code, publicUrl)}</code>
            </div>
            <button
              onClick={() => handleCopy(composeMobileCode(newCode.code, publicUrl), 'new')}
              className="text-zinc-400 hover:text-orange-500 transition-colors cursor-pointer shrink-0"
              title="Copy mobile invite code"
            >
              {copiedKey === 'new' ? <Check size={18} /> : <Copy size={18} />}
            </button>
          </div>
        </Panel>
      )}

      <Panel>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-[11px] uppercase tracking-[0.15em] text-zinc-500 border-b border-zinc-700">
                <th className="text-left py-2 pr-4 font-semibold">Code</th>
                <th className="text-left py-2 pr-4 font-semibold">Role</th>
                <th className="text-left py-2 pr-4 font-semibold">Created</th>
                <th className="text-left py-2 pr-4 font-semibold">Status</th>
                <th className="text-right py-2 font-semibold">Actions</th>
              </tr>
            </thead>
            <tbody>
              {codes.map((c) => {
                const status = codeStatus(c);
                const mobileCode = composeMobileCode(c.code, publicUrl);
                return (
                  <tr key={c.code} className="border-b border-zinc-800">
                    <td className="py-2.5 pr-4">
                      <code className="text-zinc-300 font-mono text-xs">{c.code.length > 16 ? c.code.slice(0, 16) + '...' : c.code}</code>
                    </td>
                    <td className="py-2.5 pr-4"><RoleBadge role={c.role} /></td>
                    <td className="py-2.5 pr-4 text-zinc-500">{formatDate(c.created_at)}</td>
                    <td className="py-2.5 pr-4">
                      <span className={`text-[11px] uppercase tracking-widest font-medium ${status.style}`}>
                        {status.label}
                      </span>
                    </td>
                    <td className="py-2.5 text-right">
                      <div className="flex items-center justify-end gap-2">
                        {!c.redeemed_by && (
                          <button
                            onClick={() => handleCopy(mobileCode, c.code)}
                            className="text-zinc-600 hover:text-orange-500 transition-colors cursor-pointer"
                            title="Copy mobile invite code"
                          >
                            {copiedKey === c.code ? <Check size={14} /> : <Copy size={14} />}
                          </button>
                        )}
                        {!c.redeemed_by && (
                          <button
                            onClick={() => handleDeleteCode(c.code)}
                            className="text-zinc-600 hover:text-red-500 transition-colors cursor-pointer"
                            title="Delete invite code"
                          >
                            <Trash2 size={14} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
              {codes.length === 0 && (
                <tr>
                  <td colSpan={5} className="py-6 text-center text-zinc-600 text-sm">
                    No invite codes yet
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}

// Server settings section (public URL for mobile invite codes)

function ServerSettingsSection({ publicUrl, onPublicUrlChange }: { publicUrl: string; onPublicUrlChange: (url: string) => void }) {
  const [draft, setDraft] = useState(publicUrl);
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => { setDraft(publicUrl); }, [publicUrl]);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    try {
      await putJSON('/api/settings', { server: { public_url: draft } });
      onPublicUrlChange(draft);
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch {
      setError('Failed to save. Check the URL is valid.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Panel>
      <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
        Server Settings
      </h3>
      <p className="text-sm text-zinc-600 mb-4">
        Public URL used in mobile invite codes. Leave empty to use the current browser origin.
      </p>
      {saved && (
        <div className="text-sm text-green-500 mb-3 p-2 border border-green-500/20">Saved.</div>
      )}
      {error && (
        <div className="text-sm text-red-500 mb-3 p-2 border border-red-500/20">{error}</div>
      )}
      <div className="flex gap-3 items-end">
        <div className="flex-1">
          <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
            Public URL
          </label>
          <input
            type="url"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="https://cogitator.example.com"
            className={inputClass + ' w-full'}
          />
        </div>
        <StripedButton onClick={handleSave} disabled={saving} className="h-[38px]">
          {saving ? 'Saving...' : 'Save'}
        </StripedButton>
      </div>
    </Panel>
  );
}

export default function Users() {
  const [publicUrl, setPublicUrl] = useState('');

  useEffect(() => {
    fetchJSON<Settings>('/api/settings').then((s) => {
      setPublicUrl(s.server?.public_url || '');
    }).catch(() => {});
  }, []);

  return (
    <div className="space-y-6">
      <PageHeader title="Users" subtitle="Manage users and invite codes" />
      <UsersSection />
      <ServerSettingsSection publicUrl={publicUrl} onPublicUrlChange={setPublicUrl} />
      <InviteCodesSection publicUrl={publicUrl} />
    </div>
  );
}

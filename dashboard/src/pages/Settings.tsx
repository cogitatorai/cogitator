import { useState, useEffect, useCallback } from 'react';
import { fetchJSON, putJSON, getServerUrl, clearServerUrl } from '../api';
import type { Settings, SettingsUpdateRequest } from '../api';
import { isNotificationsEnabled, setNotificationsEnabled } from '../hooks/useDesktopNotifications';
import { useAuth } from '../auth';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import StripedButton from '../components/StripedButton';

interface TelegramFormState {
  enabled: boolean;
  botToken: string;
  botTokenSet: boolean;
  allowedChatIDs: string;
}

interface VoiceFormState {
  sttProvider: string;
  ttsProvider: string;
  ttsVoice: string;
}

import type { ThemePreference } from '../useTheme';

export default function SettingsPage({ themePreference, setTheme }: { themePreference: ThemePreference; setTheme: (t: ThemePreference) => void }) {
  const { isAdmin } = useAuth();
  const [workspacePath, setWorkspacePath] = useState('');
  const [workspaceOriginal, setWorkspaceOriginal] = useState('');
  const [telegram, setTelegram] = useState<TelegramFormState>({ enabled: false, botToken: '', botTokenSet: false, allowedChatIDs: '' });
  const [allowedDomains, setAllowedDomains] = useState('');
  const [voice, setVoice] = useState<VoiceFormState>({ sttProvider: '', ttsProvider: '', ttsVoice: 'alloy' });
  const [notifEnabled, setNotifEnabled] = useState(isNotificationsEnabled);
  const [notifBlocked, setNotifBlocked] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [loading, setLoading] = useState(!isAdmin ? false : true);

  const load = useCallback(async () => {
    if (!isAdmin) return;
    try {
      const s = await fetchJSON<Settings>('/api/settings');
      setWorkspacePath(s.workspace?.path ?? '');
      setWorkspaceOriginal(s.workspace?.path ?? '');

      if (s.telegram) {
        setTelegram({
          enabled: s.telegram.enabled,
          botToken: '',
          botTokenSet: s.telegram.bot_token_set,
          allowedChatIDs: (s.telegram.allowed_chat_ids ?? []).join(', '),
        });
      }
      setAllowedDomains((s.security?.allowed_domains ?? []).join(', '));
      if (s.voice) {
        setVoice({
          sttProvider: s.voice.stt_provider ?? '',
          ttsProvider: s.voice.tts_provider ?? '',
          ttsVoice: s.voice.tts_voice || (s.voice.tts_provider ? 'alloy' : ''),
        });
      }
      setLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load settings');
      setLoading(false);
    }
  }, [isAdmin]);

  useEffect(() => { load(); }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);

    const body: SettingsUpdateRequest = {};

    if (workspacePath && workspacePath !== workspaceOriginal) {
      body.workspace = { path: workspacePath };
    }

    // Telegram settings: always send current state
    const tgUpdate: Record<string, unknown> = { enabled: telegram.enabled };
    if (telegram.botToken) {
      tgUpdate.bot_token = telegram.botToken;
    }
    const parsedIDs = telegram.allowedChatIDs
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
      .map(Number)
      .filter((n) => !isNaN(n));
    tgUpdate.allowed_chat_ids = parsedIDs;
    body.telegram = tgUpdate;

    const parsedDomains = allowedDomains.split(',').map((s) => s.trim()).filter(Boolean);
    body.security = { allowed_domains: parsedDomains };

    body.voice = {
      stt_provider: voice.sttProvider,
      tts_provider: voice.ttsProvider,
      tts_voice: voice.ttsVoice,
    };

    try {
      const updated = await putJSON<Settings>('/api/settings', body);
      setWorkspacePath(updated.workspace?.path ?? '');
      setWorkspaceOriginal(updated.workspace?.path ?? '');

      if (updated.telegram) {
        setTelegram({
          enabled: updated.telegram.enabled,
          botToken: '',
          botTokenSet: updated.telegram.bot_token_set,
          allowedChatIDs: (updated.telegram.allowed_chat_ids ?? []).join(', '),
        });
      }
      setAllowedDomains((updated.security?.allowed_domains ?? []).join(', '));
      if (updated.voice) {
        setVoice({
          sttProvider: updated.voice.stt_provider ?? '',
          ttsProvider: updated.voice.tts_provider ?? '',
          ttsVoice: updated.voice.tts_voice || (updated.voice.tts_provider ? 'alloy' : ''),
        });
      }

      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div>
        <PageHeader title="Settings" />
        <div className="text-base text-zinc-600 animate-pulse">Loading settings...</div>
      </div>
    );
  }

  return (
    <div>
      <PageHeader title="Settings" />

      {error && (
        <Panel className="border-red-500/30 mb-6">
          <p className="text-red-500 text-base">{error}</p>
        </Panel>
      )}

      {success && (
        <Panel className="border-green-500/30 mb-6">
          <p className="text-green-500 text-base">Settings saved. Provider is now active.</p>
        </Panel>
      )}

      <div className="space-y-6">
        {getServerUrl() && (
          <>
            <SectionHeader title="Server Connection" />
            <Panel>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
                Connected Server
              </h3>
              <p className="text-sm text-zinc-300 mb-3 font-mono break-all">
                {getServerUrl()}
              </p>
              <button
                onClick={() => {
                  clearServerUrl();
                  window.location.hash = 'login';
                  window.location.reload();
                }}
                className="text-[12px] uppercase tracking-widest font-medium text-red-400 hover:text-red-300 transition-colors cursor-pointer"
              >
                Disconnect
              </button>
            </Panel>
          </>
        )}

        <SectionHeader title="Appearance" />

        <Panel>
          <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
            Theme
          </h3>
          <div className="flex gap-2">
            {(['dark', 'light', 'system'] as const).map((option) => (
              <button
                key={option}
                onClick={() => setTheme(option)}
                className={`px-4 py-1.5 text-[12px] uppercase tracking-widest font-medium border transition-colors cursor-pointer ${
                  themePreference === option
                    ? 'border-orange-600 bg-orange-900/20 text-orange-500'
                    : 'border-zinc-700 text-zinc-500 hover:border-zinc-600'
                }`}
              >
                {option.charAt(0).toUpperCase() + option.slice(1)}
              </button>
            ))}
          </div>
        </Panel>

        <Panel>
          <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-3">
            Desktop Notifications
          </h3>
          <div className="flex items-center gap-3">
            <button
              onClick={async () => {
                const next = !notifEnabled;
                setNotifBlocked(false);
                const accepted = await setNotificationsEnabled(next);
                setNotifEnabled(accepted);
                if (next && !accepted) setNotifBlocked(true);
              }}
              className={`w-10 h-5 shrink-0 rounded-full relative transition-colors cursor-pointer ${
                notifEnabled ? 'bg-orange-600' : 'bg-zinc-700'
              }`}
            >
              <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-zinc-100 transition-all ${
                notifEnabled ? 'left-5' : 'left-0.5'
              }`} />
            </button>
            <span className="text-sm text-zinc-500">
              Show a notification when the agent responds or a task completes while the app is in the background
            </span>
          </div>
          {notifBlocked && (
            <p className="mt-2 text-sm text-amber-400">
              Notifications are blocked by your browser. Open your browser's site settings to allow notifications, then try again.
            </p>
          )}
        </Panel>

        {isAdmin && (
          <>
            <SectionHeader title="Workspace" />

            <Panel>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
                Data Directory
              </h3>
              <p className="text-sm text-zinc-600 mb-4">
                All data (database, memories, skills, config) is stored here.
              </p>
              <input
                type="text"
                value={workspacePath}
                onChange={(e) => setWorkspacePath(e.target.value)}
                placeholder="e.g. ~/.cogitator"
                className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600"
              />
              {workspacePath !== workspaceOriginal && (
                <p className="text-sm text-orange-500 mt-2">
                  Restart required for workspace changes to take effect.
                </p>
              )}
            </Panel>

            <SectionHeader title="Integrations" />

            <Panel>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
                Telegram
              </h3>
              <p className="text-sm text-zinc-600 mb-4">
                Connect a Telegram bot to chat with the agent from your phone.
              </p>

              <label className="flex items-center gap-2 text-base text-zinc-300 mb-4 cursor-pointer">
                <input
                  type="checkbox"
                  checked={telegram.enabled}
                  onChange={(e) => setTelegram({ ...telegram, enabled: e.target.checked })}
                  className="accent-orange-600"
                />
                Enable Telegram channel
              </label>

              <div className="space-y-4">
                <div>
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                    Bot Token
                    {telegram.botTokenSet && (
                      <span className="ml-2 text-green-600 normal-case tracking-normal font-normal">
                        (already set)
                      </span>
                    )}
                  </label>
                  <input
                    type="password"
                    value={telegram.botToken}
                    onChange={(e) => setTelegram({ ...telegram, botToken: e.target.value })}
                    placeholder={telegram.botTokenSet ? 'Leave blank to keep current token' : 'Enter bot token from @BotFather'}
                    className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600"
                  />
                </div>
                <div>
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                    Allowed Chat IDs
                  </label>
                  <input
                    type="text"
                    value={telegram.allowedChatIDs}
                    onChange={(e) => setTelegram({ ...telegram, allowedChatIDs: e.target.value })}
                    placeholder="e.g. 123456789, 987654321"
                    className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600"
                  />
                  <p className="text-sm text-zinc-600 mt-1">
                    Comma-separated numeric chat IDs. Leave empty to allow all chats during initial setup.
                  </p>
                </div>
              </div>
            </Panel>

            <SectionHeader title="Voice" />

            <Panel>
              <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
                Voice Conversation
              </h3>
              <p className="text-sm text-zinc-600 mb-4">
                Enable voice input and output in the mobile app. Requires a provider that supports speech-to-text and text-to-speech APIs.
              </p>

              <div className="space-y-4">
                <div>
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                    Speech-to-Text Provider
                  </label>
                  <select
                    value={voice.sttProvider}
                    onChange={(e) => setVoice({ ...voice, sttProvider: e.target.value })}
                    className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none"
                  >
                    <option value="">Disabled</option>
                    <option value="openai">OpenAI (Whisper)</option>
                  </select>
                </div>

                <div>
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                    Text-to-Speech Provider
                  </label>
                  <select
                    value={voice.ttsProvider}
                    onChange={(e) => setVoice({ ...voice, ttsProvider: e.target.value })}
                    className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none"
                  >
                    <option value="">Disabled</option>
                    <option value="openai">OpenAI TTS</option>
                  </select>
                </div>

                <div>
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                    Voice
                  </label>
                  <select
                    value={voice.ttsVoice}
                    onChange={(e) => setVoice({ ...voice, ttsVoice: e.target.value })}
                    disabled={!voice.ttsProvider}
                    className={`w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none ${!voice.ttsProvider ? 'opacity-40 cursor-not-allowed' : ''}`}
                  >
                    <option value="alloy">Alloy</option>
                    <option value="ash">Ash</option>
                    <option value="ballad">Ballad</option>
                    <option value="coral">Coral</option>
                    <option value="echo">Echo</option>
                    <option value="fable">Fable</option>
                    <option value="nova">Nova</option>
                    <option value="onyx">Onyx</option>
                    <option value="sage">Sage</option>
                    <option value="shimmer">Shimmer</option>
                  </select>
                  <p className="text-sm text-zinc-600 mt-1">
                    The voice used for AI responses. Requires an OpenAI-compatible TTS provider.
                  </p>
                </div>

                {voice.sttProvider && voice.ttsProvider && (
                  <p className="text-sm text-green-600">
                    Voice mode is enabled. The mobile app will show voice conversation controls.
                  </p>
                )}
              </div>
            </Panel>

            <SectionHeader title="Security" />

            <Panel>
              <h2 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-4">Network Access</h2>
              <p className="text-sm text-zinc-500 mb-4">
                Domains that network commands (curl, wget, etc.) are allowed to reach.
              </p>
              <div>
                <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                  Allowed Domains
                </label>
                <input
                  type="text"
                  value={allowedDomains}
                  onChange={(e) => setAllowedDomains(e.target.value)}
                  placeholder="e.g. api.openweathermap.org, *.github.com"
                  className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600"
                />
                <p className="text-sm text-zinc-600 mt-1">
                  Comma-separated. Supports wildcards (*.example.com). Leave empty to block all network commands.
                </p>
              </div>
            </Panel>

            <div className="flex justify-end">
              <StripedButton onClick={save} disabled={saving}>
                {saving ? 'Saving...' : 'Save Settings'}
              </StripedButton>
            </div>
          </>
        )}
      </div>
    </div>
  );
}



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

import { useState } from 'react';
import { setServerUrl } from '../api';
import StripedButton from '../components/StripedButton';

/** Decode a full invite code (base64 of "serverUrl|inviteCode"). */
function decodeInviteCode(raw: string): { serverUrl: string; inviteCode: string } | null {
  try {
    const decoded = atob(raw.trim());
    const pipe = decoded.indexOf('|');
    if (pipe < 0) return null;
    const serverUrl = decoded.slice(0, pipe).replace(/\/+$/, '');
    const inviteCode = decoded.slice(pipe + 1);
    if (!serverUrl || !inviteCode) return null;
    new URL(serverUrl); // validate
    return { serverUrl, inviteCode };
  } catch {
    return null;
  }
}

function normalizeUrl(raw: string): string | null {
  let url = raw.trim().replace(/\/+$/, '');
  if (!url) return null;
  if (!/^https?:\/\//i.test(url)) url = 'https://' + url;
  try {
    const parsed = new URL(url);
    return parsed.origin + (parsed.pathname === '/' ? '' : parsed.pathname);
  } catch {
    return null;
  }
}

/** Probe a server URL to check it's reachable. Returns null on success, or an error message. */
async function probeServer(url: string): Promise<string | null> {
  try {
    const res = await fetch(`${url}/api/auth/needs-setup`, {
      signal: AbortSignal.timeout(5000),
    });
    if (res.ok) return null;
    return `Server returned ${res.status}. Check the URL and try again.`;
  } catch (err) {
    // CORS errors and network failures both surface as TypeErrors.
    if (err instanceof TypeError) {
      return 'Could not reach server. If the server is running, it may need to be updated to support client-mode (CORS).';
    }
    return 'Could not reach server. Check the URL and try again.';
  }
}

type Step = 'choose' | 'url' | 'invite';

export default function Connect() {
  const [step, setStep] = useState<Step>('choose');
  const [serverInput, setServerInput] = useState('');
  const [inviteInput, setInviteInput] = useState('');
  const [error, setError] = useState('');
  const [connecting, setConnecting] = useState(false);

  const handleConnectUrl = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    const url = normalizeUrl(serverInput);
    if (!url) {
      setError('Please enter a valid server URL');
      return;
    }
    setConnecting(true);
    const probeErr = await probeServer(url);
    if (probeErr) {
      setConnecting(false);
      setError(probeErr);
      return;
    }
    setServerUrl(url);
    // Navigate to login; the API layer now targets the remote server.
    window.location.hash = 'login';
    window.location.reload();
  };

  const handleInviteCode = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    // Try to decode as a full invite code (base64 with server URL).
    const decoded = decodeInviteCode(inviteInput);
    if (decoded) {
      setConnecting(true);
      const probeErr = await probeServer(decoded.serverUrl);
      if (probeErr) {
        setConnecting(false);
        setError(probeErr);
        return;
      }
      setServerUrl(decoded.serverUrl);
      // Navigate to register with the plain invite code pre-filled.
      window.location.hash = `register?code=${encodeURIComponent(decoded.inviteCode)}`;
      window.location.reload();
      return;
    }

    // If it looks like a plain invite code (for the current server), just go to register.
    if (inviteInput.trim()) {
      window.location.hash = `register?code=${encodeURIComponent(inviteInput.trim())}`;
      return;
    }

    setError('Invalid invite code. Paste the full code you received.');
  };

  return (
    <div className="flex items-center justify-center min-h-screen hud-grid-bg">
      <div className="w-full max-w-sm space-y-6">
        {/* Branding */}
        <div className="text-center">
          <h1 className="text-3xl font-semibold uppercase tracking-[0.1em] text-zinc-100">
            Cogitator
          </h1>
          <div className="h-1 w-12 bg-orange-600 mt-2 mx-auto" />
          <p className="text-[12px] uppercase tracking-widest text-zinc-500 mt-3">Connect to a server</p>
        </div>

        {error && (
          <div className="border border-red-500/40 bg-red-950/20 p-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {step === 'choose' && (
          <div className="space-y-3">
            <button
              onClick={() => setStep('invite')}
              className="w-full border border-zinc-700 bg-zinc-900/80 p-4 text-left hover:border-orange-600/50 hover:bg-zinc-900 transition-colors cursor-pointer group"
            >
              <p className="text-sm font-medium uppercase tracking-widest text-zinc-200 group-hover:text-orange-500 transition-colors">
                I have an invite code
              </p>
              <p className="text-[12px] text-zinc-500 mt-1">
                Paste the invite code you received to join a server
              </p>
            </button>

            <button
              onClick={() => setStep('url')}
              className="w-full border border-zinc-700 bg-zinc-900/80 p-4 text-left hover:border-orange-600/50 hover:bg-zinc-900 transition-colors cursor-pointer group"
            >
              <p className="text-sm font-medium uppercase tracking-widest text-zinc-200 group-hover:text-orange-500 transition-colors">
                I have a server URL
              </p>
              <p className="text-[12px] text-zinc-500 mt-1">
                Enter the address of an existing Cogitator server
              </p>
            </button>
          </div>
        )}

        {step === 'url' && (
          <form onSubmit={handleConnectUrl} className="space-y-4">
            <div>
              <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
                Server URL
              </label>
              <input
                type="text"
                value={serverInput}
                onChange={(e) => setServerInput(e.target.value)}
                placeholder="https://example.com:8484"
                autoFocus
                required
                disabled={connecting}
                className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors placeholder:text-zinc-600 disabled:opacity-50"
              />
            </div>
            <StripedButton type="submit" disabled={connecting} className="w-full">
              {connecting ? 'Connecting...' : 'Connect'}
            </StripedButton>
          </form>
        )}

        {step === 'invite' && (
          <form onSubmit={handleInviteCode} className="space-y-4">
            <div>
              <label className="block text-[11px] uppercase tracking-[0.15em] font-semibold text-zinc-500 mb-1.5">
                Invite Code
              </label>
              <input
                type="text"
                value={inviteInput}
                onChange={(e) => setInviteInput(e.target.value)}
                placeholder="Paste your invite code"
                autoFocus
                required
                disabled={connecting}
                className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-sm focus:border-orange-600 focus:outline-none transition-colors placeholder:text-zinc-600 disabled:opacity-50"
              />
            </div>
            <StripedButton type="submit" disabled={connecting} className="w-full">
              {connecting ? 'Connecting...' : 'Join Server'}
            </StripedButton>
          </form>
        )}

        {step !== 'choose' && !connecting && (
          <div className="text-center">
            <button
              onClick={() => { setStep('choose'); setError(''); }}
              className="text-[12px] uppercase tracking-widest text-zinc-500 hover:text-orange-500 transition-colors cursor-pointer"
            >
              Back
            </button>
          </div>
        )}

        <div className="text-center">
          <a
            href="#login"
            className="text-[12px] uppercase tracking-widest text-zinc-500 hover:text-orange-500 transition-colors"
          >
            Already have an account? Sign in
          </a>
        </div>
      </div>
    </div>
  );
}

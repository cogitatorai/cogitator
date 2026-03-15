import { useState, useEffect, useCallback } from 'react';
import { fetchJSON, putJSON, authHeaders, fetchOllamaStatus, fetchOllamaModels, deleteOllamaModel } from '../api';
import { sendNotification } from '../hooks/useDesktopNotifications';
import type { Settings, SettingsUpdateRequest, OllamaModel, OllamaStatus } from '../api';
import Panel from './Panel';
import StripedButton from './StripedButton';

interface ProviderOption {
  value: string;
  label: string;
  models: { value: string; label: string }[];
}

function humanizeOllamaError(raw: string, model: string): string {
  const s = raw.trim();
  if (/^4\d{2}:\s*$/.test(s)) {
    return model.startsWith('hf.co/')
      ? `Ollama rejected the model (HTTP ${s.slice(0, 3)}). Try specifying a quantization tag, e.g. ${model}:Q4_K_M`
      : `Ollama rejected the model (HTTP ${s.slice(0, 3)}).`;
  }
  if (s.includes('file does not exist'))
    return `Model "${model}" was not found. Check the name and try again.`;
  if (s.includes('unauthorized'))
    return `Ollama returned "unauthorized". The model may require authentication.`;
  return s;
}

const PROVIDERS: ProviderOption[] = [
  { value: '', label: 'Select a provider...', models: [] },
  {
    value: 'openai',
    label: 'OpenAI',
    models: [
      { value: 'gpt-5.2', label: 'GPT-5.2' },
      { value: 'gpt-4.1', label: 'GPT-4.1' },
      { value: 'gpt-4.1-mini', label: 'GPT-4.1 Mini' },
      { value: 'gpt-4.1-nano', label: 'GPT-4.1 Nano' },
      { value: 'gpt-4o', label: 'GPT-4o' },
      { value: 'gpt-4o-mini', label: 'GPT-4o Mini' },
      { value: 'o3', label: 'o3' },
      { value: 'o4-mini', label: 'o4-mini' },
    ],
  },
  {
    value: 'anthropic',
    label: 'Anthropic',
    models: [
      { value: 'claude-opus-4-20250514', label: 'Claude Opus 4' },
      { value: 'claude-sonnet-4-20250514', label: 'Claude Sonnet 4' },
      { value: 'claude-haiku-4-20250414', label: 'Claude Haiku 4' },
    ],
  },
  {
    value: 'groq',
    label: 'Groq',
    models: [
      { value: 'llama-3.3-70b-versatile', label: 'Llama 3.3 70B' },
      { value: 'llama-3.1-8b-instant', label: 'Llama 3.1 8B' },
      { value: 'mixtral-8x7b-32768', label: 'Mixtral 8x7B' },
    ],
  },
  {
    value: 'together',
    label: 'Together AI',
    models: [
      { value: 'meta-llama/Llama-3.3-70B-Instruct-Turbo', label: 'Llama 3.3 70B Turbo' },
      { value: 'meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo', label: 'Llama 3.1 8B Turbo' },
      { value: 'Qwen/Qwen2.5-72B-Instruct-Turbo', label: 'Qwen 2.5 72B Turbo' },
    ],
  },
  {
    value: 'openrouter',
    label: 'OpenRouter',
    models: [
      { value: 'anthropic/claude-opus-4', label: 'Claude Opus 4' },
      { value: 'anthropic/claude-sonnet-4', label: 'Claude Sonnet 4' },
      { value: 'openai/gpt-4.1', label: 'GPT-4.1' },
      { value: 'openai/gpt-4o', label: 'GPT-4o' },
      { value: 'google/gemini-2.5-pro', label: 'Gemini 2.5 Pro' },
    ],
  },
  {
    value: 'ollama',
    label: 'Ollama (local)',
    models: [],
  },
];

const CUSTOM_VALUE = '__custom__';

function providerLabel(name: string): string {
  return PROVIDERS.find((p) => p.value === name)?.label ?? name;
}

interface ModelFormState {
  provider: string;
  model: string;
  customModel: string;
}

interface ProviderKeyState {
  apiKey: string;
  apiKeySet: boolean;
}

function resolveModel(state: ModelFormState): string {
  if (state.model === CUSTOM_VALUE) return state.customModel;
  return state.model;
}

function modelStateFromSettings(provider: string, model: string): ModelFormState {
  const providerDef = PROVIDERS.find((p) => p.value === provider);
  const knownModel = providerDef?.models.some((m) => m.value === model);
  return {
    provider,
    model: knownModel ? model : (model ? CUSTOM_VALUE : ''),
    customModel: knownModel ? '' : model,
  };
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

export default function ModelsSection() {
  const [standard, setStandard] = useState<ModelFormState>({ provider: '', model: '', customModel: '' });
  const [cheap, setCheap] = useState<ModelFormState>({ provider: '', model: '', customModel: '' });
  const [providerKeys, setProviderKeys] = useState<Record<string, ProviderKeyState>>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [loading, setLoading] = useState(true);
  const [embeddingModel, setEmbeddingModel] = useState('');
  const [embeddingModelChanged, setEmbeddingModelChanged] = useState(false);

  const load = useCallback(async () => {
    try {
      const s = await fetchJSON<Settings>('/api/settings');
      setStandard(modelStateFromSettings(s.models.standard.provider, s.models.standard.model));
      setCheap(modelStateFromSettings(s.models.cheap.provider, s.models.cheap.model));
      const keys: Record<string, ProviderKeyState> = {};
      for (const [name, ps] of Object.entries(s.providers ?? {})) {
        keys[name] = { apiKey: '', apiKeySet: ps.api_key_set };
      }
      setProviderKeys(keys);
      setEmbeddingModel(s.memory?.embedding_model ?? '');
      setLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load model settings');
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const activeProviders = [...new Set(
    [standard.provider, cheap.provider].filter((p) => p && p !== 'ollama'),
  )];

  const setProviderKey = (name: string, apiKey: string) => {
    setProviderKeys((prev) => ({
      ...prev,
      [name]: { ...prev[name], apiKey, apiKeySet: prev[name]?.apiKeySet ?? false },
    }));
  };

  const save = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);

    const body: SettingsUpdateRequest = {};
    const stdModel = resolveModel(standard);
    const chpModel = resolveModel(cheap);

    body.models = {};
    if (standard.provider || stdModel) {
      body.models.standard = {};
      if (standard.provider) body.models.standard.provider = standard.provider;
      if (stdModel) body.models.standard.model = stdModel;
    }
    if (cheap.provider || chpModel) {
      body.models.cheap = {};
      if (cheap.provider) body.models.cheap.provider = cheap.provider;
      if (chpModel) body.models.cheap.model = chpModel;
    }

    const providerUpdates: Record<string, { api_key: string }> = {};
    for (const name of activeProviders) {
      const key = providerKeys[name]?.apiKey;
      if (key) {
        providerUpdates[name] = { api_key: key };
      }
    }
    if (Object.keys(providerUpdates).length > 0) {
      body.providers = providerUpdates;
    }

    if (embeddingModelChanged && embeddingModel) {
      body.memory = { embedding_model: embeddingModel };
    }

    try {
      const updated = await putJSON<Settings>('/api/settings', body);
      setStandard(modelStateFromSettings(updated.models.standard.provider, updated.models.standard.model));
      setCheap(modelStateFromSettings(updated.models.cheap.provider, updated.models.cheap.model));
      const keys: Record<string, ProviderKeyState> = {};
      for (const [name, ps] of Object.entries(updated.providers ?? {})) {
        keys[name] = { apiKey: '', apiKeySet: ps.api_key_set };
      }
      setProviderKeys(keys);
      setEmbeddingModel(updated.memory?.embedding_model ?? '');
      setEmbeddingModelChanged(false);
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
      <>
        <SectionHeader title="Models" />
        <div className="text-base text-zinc-600 animate-pulse">Loading model settings...</div>
      </>
    );
  }

  return (
    <>
      <SectionHeader title="Models" />

      {error && (
        <Panel className="border-red-500/30">
          <p className="text-red-500 text-base">{error}</p>
        </Panel>
      )}

      {success && (
        <Panel className="border-green-500/30">
          <p className="text-green-500 text-base">Model settings saved. Provider is now active.</p>
        </Panel>
      )}

      <OllamaPanel />

      <ModelForm
        label="Primary Model (standard)"
        description="Used for conversations, reflections, and complex reasoning."
        state={standard}
        onChange={setStandard}
      />

      <ModelForm
        label="Secondary Model (cheap)"
        description="Used for enrichment, classification, and background tasks."
        state={cheap}
        onChange={setCheap}
      />

      <Panel>
        <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
          Embedding Model
        </h3>
        <p className="text-sm text-zinc-600 mb-4">
          Used for semantic memory search. Must match a model available on your provider.
        </p>
        <input
          type="text"
          value={embeddingModel}
          onChange={(e) => {
            setEmbeddingModel(e.target.value);
            setEmbeddingModelChanged(true);
          }}
          placeholder="e.g. text-embedding-3-small, nomic-embed-text"
          className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600"
        />
        {embeddingModelChanged && (
          <p className="text-[11px] text-amber-500/80 mt-2">
            Changing the embedding model will re-index all memories. Semantic search may be degraded for a few minutes.
          </p>
        )}
      </Panel>

      {activeProviders.length > 0 && (
        <Panel>
          <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
            API Keys
          </h3>
          <p className="text-sm text-zinc-600 mb-4">
            {activeProviders.length === 1
              ? 'One key is shared across both model slots.'
              : 'One key per provider. Shared automatically when both slots use the same provider.'}
          </p>
          <div className="space-y-4">
            {activeProviders.map((name) => {
              const keyState = providerKeys[name];
              return (
                <div key={name}>
                  <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
                    {providerLabel(name)}
                    {keyState?.apiKeySet && (
                      <span className="ml-2 text-green-600 normal-case tracking-normal font-normal">
                        (already set)
                      </span>
                    )}
                  </label>
                  <input
                    type="password"
                    value={keyState?.apiKey ?? ''}
                    onChange={(e) => setProviderKey(name, e.target.value)}
                    placeholder={keyState?.apiKeySet ? 'Leave blank to keep current key' : 'Enter API key'}
                    className="w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600"
                  />
                </div>
              );
            })}
          </div>
        </Panel>
      )}

      <div className="flex justify-end">
        <StripedButton onClick={save} disabled={saving}>
          {saving ? 'Saving...' : 'Save Models'}
        </StripedButton>
      </div>
    </>
  );
}

function OllamaPanel() {
  const [status, setStatus] = useState<OllamaStatus | null>(null);
  const [models, setModels] = useState<OllamaModel[]>([]);
  const [pullName, setPullName] = useState('');
  const [pulling, setPulling] = useState(false);
  const [pullingName, setPullingName] = useState('');
  const [pullProgress, setPullProgress] = useState('');
  const [pullPercent, setPullPercent] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const s = await fetchOllamaStatus();
      setStatus(s);
      if (s.running) {
        const res = await fetchOllamaModels();
        setModels(res.models ?? []);
      }
    } catch {
      setStatus({ running: false });
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  useEffect(() => {
    const id = setInterval(refresh, 10000);
    return () => clearInterval(id);
  }, [refresh]);

  const handlePull = useCallback(async () => {
    if (!pullName.trim() || pulling) return;

    let name = pullName.trim()
      .replace(/^https?:\/\/(www\.)?huggingface\.co\//, 'hf.co/');
    if (name.includes('/') && !name.startsWith('hf.co/')
        && /-gguf(:|$)/i.test(name)) {
      name = 'hf.co/' + name;
    }

    setPulling(true);
    setPullingName(name);
    setPullProgress('Starting...');
    setPullPercent(0);
    setError(null);

    try {
      const res = await fetch('/api/ollama/pull', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ name }),
      });

      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `HTTP ${res.status}`);
      }

      const reader = res.body?.getReader();
      const decoder = new TextDecoder();
      if (!reader) throw new Error('No response body');

      let buf = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });

        const lines = buf.split('\n');
        buf = lines.pop() ?? '';

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          try {
            const evt = JSON.parse(line.slice(6));
            if (evt.error) {
              const friendly = humanizeOllamaError(evt.error, name);
              setError(friendly);
              sendNotification('Model pull failed', `${name}: ${friendly}`.slice(0, 120), { page: 'admin' });
            } else if (evt.status === 'success') {
              setPullProgress('Complete');
              setPullPercent(100);
              sendNotification('Model downloaded', name, { page: 'admin' });
            } else if (evt.total && evt.total > 0) {
              const pct = Math.round((evt.completed / evt.total) * 100);
              setPullPercent(pct);
              const dlMB = (evt.completed / 1e6).toFixed(0);
              const totalMB = (evt.total / 1e6).toFixed(0);
              setPullProgress(`${dlMB} / ${totalMB} MB`);
            } else {
              setPullProgress(evt.status || 'Working...');
            }
          } catch { /* skip malformed */ }
        }
      }

      setPullName('');
      await refresh();
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Pull failed';
      setError(msg);
      sendNotification('Model pull failed', `${name}: ${msg}`.slice(0, 120), { page: 'admin' });
    } finally {
      setPulling(false);
      setPullProgress('');
      setPullPercent(0);
    }
  }, [pullName, pulling, refresh]);

  const handleDelete = useCallback(async (name: string) => {
    try {
      await deleteOllamaModel(name);
      setModels((prev) => prev.filter((m) => m.name !== name));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
    }
  }, []);

  const formatSize = (bytes: number): string => {
    if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + ' GB';
    if (bytes >= 1e6) return (bytes / 1e6).toFixed(0) + ' MB';
    return bytes + ' B';
  };

  return (
    <Panel>
      <div className="flex items-center justify-between mb-1">
        <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">
          Local Models
        </h3>
        <div className="flex items-center gap-1.5">
          <div className={`w-1.5 h-1.5 rounded-full ${status?.running ? 'bg-green-500' : 'bg-red-500'}`} />
          <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">
            {status === null ? 'Checking...' : status.running ? 'Ollama connected' : 'Ollama not detected'}
          </span>
        </div>
      </div>
      <p className="text-sm text-zinc-600 mb-4">
        Pull models from Ollama to use as local providers. Requires{' '}
        <span className="text-zinc-400">ollama</span> running on this machine.
      </p>

      {error && (
        <div className="text-sm text-red-500 mb-3 p-2 border border-red-500/20">
          {error}
        </div>
      )}

      <div className="flex gap-2 mb-4">
        <input
          type="text"
          value={pullName}
          onChange={(e) => setPullName(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handlePull()}
          placeholder="e.g. llama3.2, mistral, hf.co/user/repo:Q4_K_M"
          disabled={pulling || !status?.running}
          className="flex-1 bg-zinc-900 border border-zinc-700 p-2 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600 disabled:opacity-40"
        />
        <button
          onClick={handlePull}
          disabled={pulling || !pullName.trim() || !status?.running}
          className="px-3 py-2 text-[12px] uppercase tracking-widest font-medium border border-orange-600/50 bg-orange-900/20 text-orange-500 hover:bg-orange-900/40 hover:border-orange-500 transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {pulling ? 'Pulling...' : 'Pull'}
        </button>
      </div>

      {pulling && (
        <div className="mb-4 p-3 border border-orange-600/30 bg-orange-900/10">
          <div className="flex items-center justify-between mb-2">
            <span className="text-[12px] uppercase tracking-widest font-medium text-orange-500">
              Pulling {pullingName}
            </span>
            <span className="text-[12px] uppercase tracking-widest font-medium text-zinc-500">
              {pullProgress || 'Connecting...'}
            </span>
          </div>
          <div className="h-1.5 bg-zinc-800 overflow-hidden">
            <div
              className={`h-full transition-all duration-300 ${
                pullPercent > 0 ? 'bg-orange-600' : 'bg-orange-600/50 animate-pulse'
              }`}
              style={{ width: pullPercent > 0 ? `${pullPercent}%` : '100%' }}
            />
          </div>
          {pullPercent > 0 && (
            <div className="text-right mt-1">
              <span className="text-[11px] text-zinc-500">{pullPercent}%</span>
            </div>
          )}
        </div>
      )}

      {models.length > 0 ? (
        <div className="space-y-1">
          {models.map((m) => (
            <div key={m.name} className="flex items-center justify-between p-2 border border-zinc-700 hover:border-zinc-600 transition-colors">
              <div className="flex-1 min-w-0">
                <span className="text-base text-zinc-300">{m.name}</span>
                <div className="flex gap-3 mt-0.5">
                  {m.parameter_size && (
                    <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">
                      {m.parameter_size}
                    </span>
                  )}
                  <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">
                    {formatSize(m.size)}
                  </span>
                  {m.quantization_level && (
                    <span className="text-[11px] uppercase tracking-widest font-medium text-zinc-600">
                      {m.quantization_level}
                    </span>
                  )}
                </div>
              </div>
              <button
                onClick={() => handleDelete(m.name)}
                className="text-[11px] uppercase tracking-widest font-medium text-zinc-600 hover:text-red-500 transition-colors cursor-pointer px-2"
              >
                Delete
              </button>
            </div>
          ))}
        </div>
      ) : status?.running ? (
        <p className="text-sm text-zinc-600">No local models. Pull a model to get started.</p>
      ) : null}
    </Panel>
  );
}

function ModelForm({ label, description, state, onChange }: {
  label: string;
  description: string;
  state: ModelFormState;
  onChange: (s: ModelFormState) => void;
}) {
  const [ollamaModels, setOllamaModels] = useState<{ value: string; label: string }[]>([]);

  useEffect(() => {
    if (state.provider !== 'ollama') return;
    fetchOllamaModels()
      .then((res) => {
        setOllamaModels(
          (res.models ?? []).map((m) => ({
            value: m.name,
            label: m.name + (m.parameter_size ? ` (${m.parameter_size})` : ''),
          }))
        );
      })
      .catch(() => setOllamaModels([]));
  }, [state.provider]);

  const providerDef = PROVIDERS.find((p) => p.value === state.provider);
  const models = state.provider === 'ollama' ? ollamaModels : (providerDef?.models ?? []);
  const showCustomInput = state.model === CUSTOM_VALUE;

  const selectClass =
    'w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none';
  const inputClass =
    'w-full bg-zinc-900 border border-zinc-700 p-2.5 text-zinc-300 text-base focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none placeholder:text-zinc-600';

  return (
    <Panel>
      <h3 className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 mb-1">
        {label}
      </h3>
      <p className="text-sm text-zinc-600 mb-4">{description}</p>

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
            Provider
          </label>
          <select
            value={state.provider}
            onChange={(e) => onChange({ ...state, provider: e.target.value, model: '', customModel: '' })}
            className={selectClass}
          >
            {PROVIDERS.map((p) => (
              <option key={p.value} value={p.value}>{p.label}</option>
            ))}
          </select>
        </div>

        <div>
          <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
            Model
          </label>
          {models.length > 0 ? (
            <select
              value={state.model}
              onChange={(e) => onChange({ ...state, model: e.target.value, customModel: '' })}
              className={selectClass}
            >
              <option value="">Select a model...</option>
              {models.map((m) => (
                <option key={m.value} value={m.value}>{m.label}</option>
              ))}
              <option value={CUSTOM_VALUE}>Custom...</option>
            </select>
          ) : (
            <input
              type="text"
              value={state.customModel}
              onChange={(e) => onChange({ ...state, model: CUSTOM_VALUE, customModel: e.target.value })}
              placeholder="Enter model identifier"
              className={inputClass}
            />
          )}
        </div>
      </div>

      {showCustomInput && models.length > 0 && (
        <div className="mt-4">
          <label className="text-[12px] uppercase tracking-widest font-medium text-zinc-500 block mb-1.5">
            Custom Model ID
          </label>
          <input
            type="text"
            value={state.customModel}
            onChange={(e) => onChange({ ...state, customModel: e.target.value })}
            placeholder="e.g. gpt-4.1-2025-04-14"
            className={inputClass}
          />
        </div>
      )}
    </Panel>
  );
}

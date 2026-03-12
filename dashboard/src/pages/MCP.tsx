import { useState, useEffect, useCallback } from 'react';
import { Plus, Play, Square, Trash2, ChevronDown, ChevronRight, X } from 'lucide-react';
import {
  usePolling,
  fetchMCPServers,
  addMCPServer,
  removeMCPServer,
  startMCPServer,
  stopMCPServer,
  fetchMCPTools,
  testMCPTool,
  updateMCPServerSecrets,
  updateMCPServer,
} from '../api';
import type { MCPServer, MCPTool, MCPToolTestResult } from '../api';
import { useWebSocket } from '../ws';
import PageHeader from '../components/PageHeader';

const STATUS_DOT: Record<MCPServer['status'], string> = {
  stopped: 'bg-zinc-500',
  starting: 'bg-amber-500 animate-pulse',
  running: 'bg-green-500',
  reconnecting: 'bg-amber-500 animate-pulse',
  error: 'bg-red-500',
};

export default function MCP() {
  const [expanded, setExpanded] = useState<string | null>(null);
  const [tools, setTools] = useState<Record<string, MCPTool[]>>({});
  const [loadingTools, setLoadingTools] = useState<string | null>(null);

  // Add Server modal
  const [showAdd, setShowAdd] = useState(false);
  const [addName, setAddName] = useState('');
  const [addCommand, setAddCommand] = useState('');
  const [addArgs, setAddArgs] = useState('');
  const [addEnv, setAddEnv] = useState<{ key: string; value: string }[]>([]);
  const [addTab, setAddTab] = useState<'local' | 'remote'>('local');
  const [addUrl, setAddUrl] = useState('');
  const [addTransport, setAddTransport] = useState<'streamable-http' | 'sse'>('streamable-http');
  const [addHeaders, setAddHeaders] = useState<{ key: string; value: string }[]>([]);
  const [showAuth, setShowAuth] = useState(false);
  const [addOAuthClientId, setAddOAuthClientId] = useState('');
  const [addOAuthClientSecret, setAddOAuthClientSecret] = useState('');
  const [addOAuthScopes, setAddOAuthScopes] = useState('');
  const [addInstructions, setAddInstructions] = useState('');
  const [addSubmitting, setAddSubmitting] = useState(false);

  // Inline instructions editing
  const [editingInstructions, setEditingInstructions] = useState<Record<string, string>>({});

  // Tool test modal
  const [testServer, setTestServer] = useState<string | null>(null);
  const [testTool, setTestTool] = useState<MCPTool | null>(null);
  const [testArgs, setTestArgs] = useState('{}');
  const [testRunning, setTestRunning] = useState(false);
  const [testResult, setTestResult] = useState<MCPToolTestResult | null>(null);

  const { subscribe, unsubscribe } = useWebSocket();

  const { data, refresh } = usePolling<{ servers: MCPServer[] }>(
    fetchMCPServers,
    5000,
  );

  const servers = data?.servers ?? [];

  // Listen for real-time MCP state changes
  useEffect(() => {
    const listener = (msg: { type: string; [key: string]: unknown }) => {
      if (msg.type === 'mcp_server_state') {
        refresh();
      }
    };
    subscribe(listener);
    return () => unsubscribe(listener);
  }, [subscribe, unsubscribe, refresh]);

  const handleExpand = useCallback(async (name: string) => {
    if (expanded === name) {
      setExpanded(null);
      return;
    }
    setExpanded(name);
    if (!tools[name]) {
      setLoadingTools(name);
      try {
        const res = await fetchMCPTools(name);
        setTools((prev) => ({ ...prev, [name]: res.tools ?? [] }));
      } catch {
        setTools((prev) => ({ ...prev, [name]: [] }));
      }
      setLoadingTools(null);
    }
  }, [expanded, tools]);

  const handleStart = useCallback(async (name: string) => {
    try {
      await startMCPServer(name);
      refresh();
    } catch (e) {
      console.error('start failed', e);
    }
  }, [refresh]);

  const handleStop = useCallback(async (name: string) => {
    try {
      await stopMCPServer(name);
      refresh();
    } catch (e) {
      console.error('stop failed', e);
    }
  }, [refresh]);

  const handleRemove = useCallback(async (name: string) => {
    try {
      await removeMCPServer(name);
      if (expanded === name) setExpanded(null);
      refresh();
    } catch (e) {
      console.error('remove failed', e);
    }
  }, [expanded, refresh]);

  const resetAddForm = () => {
    setAddName('');
    setAddCommand('');
    setAddArgs('');
    setAddEnv([]);
    setAddTab('local');
    setAddUrl('');
    setAddTransport('streamable-http');
    setAddHeaders([]);
    setShowAuth(false);
    setAddOAuthClientId('');
    setAddOAuthClientSecret('');
    setAddOAuthScopes('');
    setAddInstructions('');
    setAddSubmitting(false);
  };

  const handleAdd = useCallback(async () => {
    if (!addName.trim()) return;

    setAddSubmitting(true);
    try {
      if (addTab === 'local') {
        if (!addCommand.trim()) { setAddSubmitting(false); return; }
        const args = addArgs.trim() ? addArgs.trim().split(/\s+/) : undefined;
        const env: Record<string, string> = {};
        for (const pair of addEnv) {
          if (pair.key.trim()) env[pair.key.trim()] = pair.value;
        }
        await addMCPServer({
          name: addName.trim(),
          command: addCommand.trim(),
          args,
          env: Object.keys(env).length > 0 ? env : undefined,
          instructions: addInstructions || undefined,
        });
      } else {
        if (!addUrl.trim()) { setAddSubmitting(false); return; }
        const configHeaders: Record<string, string> = {};
        for (const h of addHeaders) {
          if (h.key.trim()) configHeaders[h.key.trim()] = h.value;
        }
        await addMCPServer({
          name: addName.trim(),
          url: addUrl.trim(),
          transport: addTransport,
          headers: Object.keys(configHeaders).length > 0 ? configHeaders : undefined,
          instructions: addInstructions || undefined,
        });

        const hasOAuth = addOAuthClientId.trim() || addOAuthClientSecret.trim();
        if (hasOAuth) {
          const scopes = addOAuthScopes.trim()
            ? addOAuthScopes.trim().split(',').map((s) => s.trim())
            : undefined;
          await updateMCPServerSecrets(addName.trim(), {
            oauth: {
              client_id: addOAuthClientId.trim(),
              client_secret: addOAuthClientSecret.trim(),
              scopes,
            },
          });
        }
      }

      setShowAdd(false);
      resetAddForm();
      refresh();
    } catch (e) {
      console.error('add failed', e);
      setAddSubmitting(false);
    }
  }, [addName, addTab, addCommand, addArgs, addEnv, addUrl, addTransport, addHeaders, addOAuthClientId, addOAuthClientSecret, addOAuthScopes, addInstructions, refresh]);

  const openTest = (serverName: string, tool: MCPTool) => {
    setTestServer(serverName);
    setTestTool(tool);
    setTestArgs('{}');
    setTestResult(null);
    setTestRunning(false);
  };

  const handleTest = useCallback(async () => {
    if (!testServer || !testTool) return;
    setTestRunning(true);
    setTestResult(null);
    try {
      const parsed = JSON.parse(testArgs);
      const result = await testMCPTool(testServer, testTool.name, parsed);
      setTestResult(result);
    } catch (e) {
      setTestResult({
        result: '',
        duration_ms: 0,
        error: e instanceof Error ? e.message : 'Unknown error',
      });
    }
    setTestRunning(false);
  }, [testServer, testTool, testArgs]);

  const closeTest = () => {
    setTestServer(null);
    setTestTool(null);
    setTestResult(null);
  };

  return (
    <div>
      <PageHeader title="MCP Servers" subtitle="Manage Model Context Protocol server integrations" />

      {/* Empty state */}
      {servers.length === 0 && (
        <div className="border border-zinc-800 bg-zinc-900/50 p-8 text-center">
          <p className="text-sm text-zinc-500 mb-4 uppercase tracking-widest">
            No MCP servers configured
          </p>
          <button
            onClick={() => setShowAdd(true)}
            className="bg-orange-900/20 border border-orange-600/50 hover:border-orange-500 hover:bg-orange-900/40 text-orange-500 hover:text-orange-400 uppercase font-mono tracking-widest text-xs font-bold px-4 py-2 cursor-pointer"
          >
            Add Server
          </button>
        </div>
      )}

      {/* Server list */}
      {servers.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center justify-between mb-4">
            <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500">
              {servers.length} server{servers.length !== 1 ? 's' : ''}
            </span>
            <button
              onClick={() => setShowAdd(true)}
              className="flex items-center gap-1.5 border border-zinc-700 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 uppercase font-mono tracking-widest text-xs font-bold px-3 py-2 cursor-pointer"
            >
              <Plus size={12} />
              Add
            </button>
          </div>

          {servers.map((srv) => {
            const isExpanded = expanded === srv.name;
            const serverTools = tools[srv.name];
            return (
              <div key={srv.name} className="border border-zinc-800 bg-zinc-900/50">
                {/* Server row */}
                <div
                  onClick={() => handleExpand(srv.name)}
                  className="flex items-center gap-3 p-4 cursor-pointer hover:bg-zinc-800/30 transition-colors"
                >
                  {isExpanded
                    ? <ChevronDown size={14} className="text-zinc-600 shrink-0" />
                    : <ChevronRight size={14} className="text-zinc-600 shrink-0" />
                  }
                  <span className={`w-2 h-2 rounded-full shrink-0 ${STATUS_DOT[srv.status]}`} />
                  <span className="text-sm font-mono text-zinc-100 flex-1 truncate">
                    {srv.name}
                  </span>
                  {srv.remote && (
                    <span className="text-[9px] px-1.5 py-0.5 uppercase tracking-widest font-bold bg-blue-500/10 text-blue-400 border border-blue-500/20">
                      Remote
                    </span>
                  )}
                  {srv.tool_count > 0 && (
                    <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-600">
                      {srv.tool_count} tool{srv.tool_count !== 1 ? 's' : ''}
                    </span>
                  )}
                  <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500">
                    {srv.status}
                  </span>
                  <div className="flex items-center gap-1 shrink-0" onClick={(e) => e.stopPropagation()}>
                    {(srv.status === 'stopped' || srv.status === 'error') && (
                      <button
                        onClick={() => handleStart(srv.name)}
                        title="Start"
                        className="border border-zinc-700 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 p-1.5 cursor-pointer"
                      >
                        <Play size={12} />
                      </button>
                    )}
                    {(srv.status === 'running' || srv.status === 'starting' || srv.status === 'reconnecting') && (
                      <button
                        onClick={() => handleStop(srv.name)}
                        title="Stop"
                        className="border border-zinc-700 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 p-1.5 cursor-pointer"
                      >
                        <Square size={12} />
                      </button>
                    )}
                    <button
                      onClick={() => handleRemove(srv.name)}
                      title="Remove"
                      className="border border-red-600/50 text-red-500 bg-red-900/20 hover:bg-red-900/40 p-1.5 cursor-pointer"
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                </div>

                {/* Expanded detail */}
                {isExpanded && (
                  <div className="border-t border-zinc-800 p-4 space-y-4">
                    <div className="flex gap-6">
                      <div>
                        <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                          {srv.remote ? 'URL' : 'Command'}
                        </span>
                        <span className="text-sm font-mono text-zinc-400">
                          {srv.remote ? srv.url : `${srv.command} ${srv.args?.join(' ') ?? ''}`}
                        </span>
                      </div>
                      {srv.remote && srv.transport && (
                        <div>
                          <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                            Transport
                          </span>
                          <span className="text-sm font-mono text-zinc-400">
                            {srv.transport === 'sse' ? 'SSE' : 'Streamable HTTP'}
                          </span>
                        </div>
                      )}
                      {srv.started_at && (
                        <div>
                          <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                            Started
                          </span>
                          <span className="text-sm text-zinc-400">
                            {new Date(srv.started_at).toLocaleString([], {
                              month: 'short',
                              day: 'numeric',
                              hour: '2-digit',
                              minute: '2-digit',
                            })}
                          </span>
                        </div>
                      )}
                    </div>

                    {srv.error && (
                      <div className="border border-red-600/30 bg-red-900/10 p-3">
                        <span className="text-[10px] uppercase tracking-widest font-bold text-red-500 block mb-1">
                          Error
                        </span>
                        <p className="text-sm font-mono text-red-400">{srv.error}</p>
                      </div>
                    )}

                    {/* Tools */}
                    <div>
                      <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-2">
                        Tools
                      </span>
                      {loadingTools === srv.name ? (
                        <p className="text-sm text-zinc-600">Loading tools...</p>
                      ) : serverTools && serverTools.length > 0 ? (
                        <div className="space-y-1">
                          {serverTools.map((tool) => (
                            <div
                              key={tool.qualified_name}
                              className="flex items-start justify-between gap-3 p-2 border border-zinc-800 hover:border-zinc-700 transition-colors"
                            >
                              <div className="min-w-0">
                                <span className="text-sm font-mono text-zinc-300 block">
                                  {tool.name}
                                </span>
                                {tool.description && (
                                  <p className="text-xs text-zinc-500 mt-0.5 line-clamp-2">
                                    {tool.description}
                                  </p>
                                )}
                              </div>
                              <button
                                onClick={() => openTest(srv.name, tool)}
                                className="shrink-0 border border-zinc-700 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 uppercase font-mono tracking-widest text-xs font-bold px-2 py-1 cursor-pointer"
                              >
                                Test
                              </button>
                            </div>
                          ))}
                        </div>
                      ) : srv.status === 'running' ? (
                        <p className="text-sm text-zinc-600">No tools discovered.</p>
                      ) : (
                        <p className="text-sm text-zinc-600">Start the server to discover tools.</p>
                      )}
                    </div>

                    {/* Agent Instructions */}
                    <div>
                      <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                        Agent Instructions
                      </span>
                      <textarea
                        className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono resize-y"
                        rows={2}
                        placeholder="No instructions configured. Describe what this server does..."
                        value={editingInstructions[srv.name] ?? srv.instructions ?? ''}
                        onChange={e => setEditingInstructions(prev => ({ ...prev, [srv.name]: e.target.value }))}
                        onBlur={async () => {
                          const val = editingInstructions[srv.name];
                          if (val !== undefined && val !== (srv.instructions ?? '')) {
                            try {
                              await updateMCPServer(srv.name, { instructions: val });
                              refresh();
                            } catch (e) { console.error('update instructions failed', e); }
                          }
                          setEditingInstructions(prev => {
                            const next = { ...prev };
                            delete next[srv.name];
                            return next;
                          });
                        }}
                      />
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* Add Server Modal */}
      {showAdd && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="w-full max-w-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
            <div className="flex items-center justify-between p-4 border-b border-zinc-800">
              <h3 className="text-sm uppercase tracking-widest font-bold text-zinc-100">
                Add MCP Server
              </h3>
              <button
                onClick={() => { setShowAdd(false); resetAddForm(); }}
                className="text-zinc-500 hover:text-zinc-300 cursor-pointer"
              >
                <X size={16} />
              </button>
            </div>
            <div className="p-4 space-y-4">
              <div>
                <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                  Name
                </label>
                <input
                  type="text"
                  value={addName}
                  onChange={(e) => setAddName(e.target.value)}
                  placeholder="my-server"
                  className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                />
              </div>

              {/* Tab bar */}
              <div className="flex border-b border-zinc-700">
                <button
                  className={`px-4 py-2 text-[10px] uppercase tracking-widest font-bold border-b-2 transition-colors cursor-pointer ${
                    addTab === 'local'
                      ? 'border-orange-500 text-zinc-100'
                      : 'border-transparent text-zinc-500 hover:text-zinc-300'
                  }`}
                  onClick={() => setAddTab('local')}
                >
                  Local
                </button>
                <button
                  className={`px-4 py-2 text-[10px] uppercase tracking-widest font-bold border-b-2 transition-colors cursor-pointer ${
                    addTab === 'remote'
                      ? 'border-orange-500 text-zinc-100'
                      : 'border-transparent text-zinc-500 hover:text-zinc-300'
                  }`}
                  onClick={() => setAddTab('remote')}
                >
                  Remote
                </button>
              </div>

              {addTab === 'local' ? (
                <>
                  <div>
                    <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                      Command
                    </label>
                    <input
                      type="text"
                      value={addCommand}
                      onChange={(e) => setAddCommand(e.target.value)}
                      placeholder="npx -y @modelcontextprotocol/server-example"
                      className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                    />
                  </div>
                  <div>
                    <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                      Arguments (space separated)
                    </label>
                    <input
                      type="text"
                      value={addArgs}
                      onChange={(e) => setAddArgs(e.target.value)}
                      placeholder="--port 3000"
                      className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                    />
                  </div>
                  <div>
                    <div className="flex items-center justify-between mb-1">
                      <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500">
                        Environment Variables
                      </label>
                      <button
                        onClick={() => setAddEnv((prev) => [...prev, { key: '', value: '' }])}
                        className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 hover:text-zinc-300 cursor-pointer"
                      >
                        + Add
                      </button>
                    </div>
                    {addEnv.length === 0 && (
                      <p className="text-xs text-zinc-600">None</p>
                    )}
                    {addEnv.map((pair, i) => (
                      <div key={i} className="flex items-center gap-2 mt-1">
                        <input
                          type="text"
                          value={pair.key}
                          onChange={(e) => {
                            const next = [...addEnv];
                            next[i] = { ...next[i], key: e.target.value };
                            setAddEnv(next);
                          }}
                          placeholder="KEY"
                          className="flex-1 border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                        />
                        <input
                          type="text"
                          value={pair.value}
                          onChange={(e) => {
                            const next = [...addEnv];
                            next[i] = { ...next[i], value: e.target.value };
                            setAddEnv(next);
                          }}
                          placeholder="value"
                          className="flex-1 border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                        />
                        <button
                          onClick={() => setAddEnv((prev) => prev.filter((_, j) => j !== i))}
                          className="text-zinc-500 hover:text-red-400 cursor-pointer"
                        >
                          <X size={14} />
                        </button>
                      </div>
                    ))}
                  </div>
                  <div>
                    <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                      Instructions (optional)
                    </label>
                    <textarea
                      className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono resize-y"
                      rows={2}
                      placeholder="Describe what this server does and when the agent should use its tools..."
                      value={addInstructions}
                      onChange={e => setAddInstructions(e.target.value)}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div>
                    <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                      URL
                    </label>
                    <input
                      type="text"
                      value={addUrl}
                      onChange={(e) => setAddUrl(e.target.value)}
                      placeholder="https://mcp.example.com/v1"
                      className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                    />
                  </div>
                  <div>
                    <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                      Transport
                    </label>
                    <select
                      value={addTransport}
                      onChange={(e) => setAddTransport(e.target.value as 'streamable-http' | 'sse')}
                      className="w-full border border-zinc-700 bg-zinc-900 text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                    >
                      <option value="streamable-http">Streamable HTTP</option>
                      <option value="sse">SSE</option>
                    </select>
                  </div>

                  {/* Authentication (collapsible) */}
                  <button
                    className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 hover:text-zinc-300 flex items-center gap-1 cursor-pointer"
                    onClick={() => setShowAuth(!showAuth)}
                  >
                    {showAuth ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                    Authentication
                  </button>
                  {showAuth && (
                    <div className="border border-zinc-700 p-3 space-y-3">
                      <div>
                        <div className="flex items-center justify-between mb-1">
                          <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500">
                            Headers
                          </span>
                          <button
                            className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 hover:text-zinc-300 cursor-pointer"
                            onClick={() => setAddHeaders([...addHeaders, { key: '', value: '' }])}
                          >
                            + Add
                          </button>
                        </div>
                        {addHeaders.length === 0 && (
                          <p className="text-xs text-zinc-600">None</p>
                        )}
                        {addHeaders.map((h, i) => (
                          <div key={i} className="flex items-center gap-2 mt-1">
                            <input
                              type="text"
                              value={h.key}
                              onChange={(e) => {
                                const next = [...addHeaders];
                                next[i] = { ...next[i], key: e.target.value };
                                setAddHeaders(next);
                              }}
                              placeholder="Header name"
                              className="flex-1 border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                            />
                            <input
                              type="text"
                              value={h.value}
                              onChange={(e) => {
                                const next = [...addHeaders];
                                next[i] = { ...next[i], value: e.target.value };
                                setAddHeaders(next);
                              }}
                              placeholder="Value"
                              className="flex-1 border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                            />
                            <button
                              onClick={() => setAddHeaders(addHeaders.filter((_, j) => j !== i))}
                              className="text-zinc-500 hover:text-red-400 cursor-pointer"
                            >
                              <X size={14} />
                            </button>
                          </div>
                        ))}
                      </div>

                      <div className="space-y-2">
                        <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500">
                          OAuth 2.0
                        </span>
                        <input
                          type="text"
                          value={addOAuthClientId}
                          onChange={(e) => setAddOAuthClientId(e.target.value)}
                          placeholder="Client ID"
                          className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                        />
                        <input
                          type="password"
                          value={addOAuthClientSecret}
                          onChange={(e) => setAddOAuthClientSecret(e.target.value)}
                          placeholder="Client Secret"
                          className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                        />
                        <input
                          type="text"
                          value={addOAuthScopes}
                          onChange={(e) => setAddOAuthScopes(e.target.value)}
                          placeholder="Scopes (comma-separated)"
                          className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-1.5 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono"
                        />
                      </div>
                    </div>
                  )}
                  <div>
                    <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                      Instructions (optional)
                    </label>
                    <textarea
                      className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono resize-y"
                      rows={2}
                      placeholder="Describe what this server does and when the agent should use its tools..."
                      value={addInstructions}
                      onChange={e => setAddInstructions(e.target.value)}
                    />
                  </div>
                </>
              )}
            </div>
            <div className="flex items-center justify-end gap-2 p-4 border-t border-zinc-800">
              <button
                onClick={() => { setShowAdd(false); resetAddForm(); }}
                className="border border-zinc-700 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 uppercase font-mono tracking-widest text-xs font-bold px-3 py-2 cursor-pointer"
              >
                Cancel
              </button>
              <button
                onClick={handleAdd}
                disabled={addSubmitting || !addName.trim() || (addTab === 'local' ? !addCommand.trim() : !addUrl.trim())}
                className="bg-orange-900/20 border border-orange-600/50 hover:border-orange-500 hover:bg-orange-900/40 text-orange-500 hover:text-orange-400 uppercase font-mono tracking-widest text-xs font-bold px-4 py-2 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
              >
                {addSubmitting ? 'Adding...' : 'Add'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Tool Test Modal */}
      {testTool && testServer && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="w-full max-w-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
            <div className="flex items-center justify-between p-4 border-b border-zinc-800">
              <div>
                <h3 className="text-sm uppercase tracking-widest font-bold text-zinc-100">
                  Test Tool
                </h3>
                <p className="text-xs font-mono text-zinc-500 mt-1">
                  {testServer} / {testTool.name}
                </p>
              </div>
              <button
                onClick={closeTest}
                className="text-zinc-500 hover:text-zinc-300 cursor-pointer"
              >
                <X size={16} />
              </button>
            </div>
            <div className="p-4 space-y-4">
              {testTool.description && (
                <p className="text-xs text-zinc-500">{testTool.description}</p>
              )}
              <div>
                <label className="text-[10px] uppercase tracking-widest font-bold text-zinc-500 block mb-1">
                  Arguments (JSON)
                </label>
                <textarea
                  value={testArgs}
                  onChange={(e) => setTestArgs(e.target.value)}
                  rows={6}
                  className="w-full border border-zinc-700 bg-transparent text-zinc-300 text-sm px-3 py-2 focus:border-orange-600 focus:ring-1 focus:ring-orange-600/20 focus:outline-none font-mono resize-y"
                />
              </div>

              {testResult && (
                <div className="space-y-2">
                  {testResult.error ? (
                    <div className="border border-red-600/30 bg-red-900/10 p-3">
                      <span className="text-[10px] uppercase tracking-widest font-bold text-red-500 block mb-1">
                        Error
                      </span>
                      <p className="text-sm font-mono text-red-400 whitespace-pre-wrap">{testResult.error}</p>
                    </div>
                  ) : (
                    <div className="border border-zinc-800 bg-zinc-900/80 p-3">
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-500">
                          Result
                        </span>
                        <span className="text-[10px] uppercase tracking-widest font-bold text-zinc-600">
                          {testResult.duration_ms}ms
                        </span>
                      </div>
                      <pre className="text-sm font-mono text-zinc-300 whitespace-pre-wrap break-all max-h-64 overflow-y-auto">
                        {testResult.result}
                      </pre>
                    </div>
                  )}
                </div>
              )}
            </div>
            <div className="flex items-center justify-end gap-2 p-4 border-t border-zinc-800">
              <button
                onClick={closeTest}
                className="border border-zinc-700 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50 uppercase font-mono tracking-widest text-xs font-bold px-3 py-2 cursor-pointer"
              >
                Cancel
              </button>
              <button
                onClick={handleTest}
                disabled={testRunning}
                className="bg-orange-900/20 border border-orange-600/50 hover:border-orange-500 hover:bg-orange-900/40 text-orange-500 hover:text-orange-400 uppercase font-mono tracking-widest text-xs font-bold px-4 py-2 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
              >
                {testRunning ? 'Running...' : 'Run'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

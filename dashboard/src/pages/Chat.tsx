import { useState, useEffect, useRef, useCallback, useMemo, memo } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { fetchJSON, putJSON, deleteJSON, sendChatMessage, markAllNotificationsRead } from '../api';
import type { Session, SessionDetail, PersistedMessage } from '../api';
import { useWebSocket } from '../ws';
import type { WsMessage } from '../ws';
import { Lock, Paperclip, ListTodo } from 'lucide-react';
import Panel from '../components/Panel';
import PageHeader from '../components/PageHeader';
import SessionSidebar from '../components/SessionSidebar';
import { sendNotification } from '../hooks/useDesktopNotifications';

interface ResolvedTools {
  skills?: string[];
  tools?: string[];
  memory?: string[];
}

interface ChatMessage {
  id?: number;
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: Date;
  toolsUsed?: ResolvedTools;
}

interface Activity {
  status: 'thinking' | 'tool_calling';
  tool?: string;
}

const TOOL_LABELS: Record<string, string> = {
  search_skills: 'Searching skills',
  install_skill: 'Installing skill',
  read_skill: 'Reading skill',
  list_installed_skills: 'Listing skills',
  shell: 'Running command',
  read_file: 'Reading file',
  write_file: 'Writing file',
  list_directory: 'Listing directory',
  create_task: 'Creating task',
  list_tasks: 'Listing tasks',
  run_task: 'Running task',
  delete_task: 'Deleting task',
};

const SUGGESTIONS = [
  "Let's get to know each other",
  'What can you do?',
  'Remember that I prefer dark roast coffee',
  'Every morning at 8am, summarize Hacker News',
  'What do you know about me?',
  'Find a skill for checking the weather',
  'Help me plan my week',
];

// Parse the pre-resolved tools_used JSON from a persisted message.
function parseToolsUsed(json: string | undefined): ResolvedTools | undefined {
  if (!json) return undefined;
  try {
    const parsed = JSON.parse(json) as ResolvedTools;
    if (parsed.skills?.length || parsed.tools?.length || parsed.memory?.length) return parsed;
    return undefined;
  } catch {
    return undefined;
  }
}

// Cheap fingerprint: last message ID + count. Avoids re-rendering when
// a session_update fires but message list hasn't actually changed.
function messagesFingerprint(msgs: ChatMessage[]): string {
  if (msgs.length === 0) return '0';
  const last = msgs[msgs.length - 1];
  return `${msgs.length}:${last.id ?? ''}:${last.content.length}`;
}

function parseSessionKey(): string | null {
  const hash = window.location.hash.replace('#', '');
  const parts = hash.split('/');
  if (parts.length >= 2 && parts[0] === 'chat') {
    return decodeURIComponent(parts.slice(1).join('/'));
  }
  return null;
}

function navigateToSession(key: string) {
  window.location.hash = `chat/${encodeURIComponent(key)}`;
}

export default function Chat() {
  const { connected, connecting, connect, send: wsSend, setSessionKey: wsSetSessionKey, subscribe, unsubscribe } = useWebSocket();

  const [sessionKey, setSessionKey] = useState<string | null>(() => {
    return parseSessionKey() || sessionStorage.getItem('chat-active-session');
  });
  const [sessions, setSessions] = useState<Session[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState(
    () => sessionStorage.getItem('chat-draft') ?? '',
  );
  const [activity, setActivity] = useState<Activity | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(
    () => localStorage.getItem('chat-sidebar-collapsed') === 'true',
  );
  const [pendingPrivate, setPendingPrivate] = useState<Set<string>>(() => new Set());
  const [hasMemories, setHasMemories] = useState<boolean | null>(null);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [pinnedSession, setPinnedSession] = useState<Session | null>(null);
  const [unreadSessions, setUnreadSessions] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    fetchJSON<SessionDetail>('/api/sessions/tasks:output').then((d) => setPinnedSession(d.session)).catch(() => {});
    fetchJSON<{ total_nodes: number }>('/api/memory/stats').then((s) => setHasMemories(s.total_nodes > 0)).catch(() => setHasMemories(null));
  }, []);

  const ACCEPTED_FILE_TYPES = '.png,.jpg,.jpeg,.gif,.webp,.pdf,.docx,.txt,.md,.csv';
  const MAX_FILE_SIZE = 10 * 1024 * 1024;

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const loadGenRef = useRef(0);

  // Convert raw persisted messages into ChatMessages with toolsUsed attached.
  const toChatMessages = useCallback((raw: PersistedMessage[]): ChatMessage[] => {
    return raw
      .filter((m) => (m.role === 'user' || m.role === 'assistant' || m.role === 'system') && m.content?.trim())
      .map((m) => {
        let content = m.content;
        // Multimodal messages are stored as JSON arrays of content blocks.
        // Extract only the user-facing text parts for display.
        if (content.trimStart().startsWith('[')) {
          try {
            const blocks = JSON.parse(content) as { type: string; text?: string }[];
            if (Array.isArray(blocks)) {
              const texts: string[] = [];
              for (const b of blocks) {
                if (b.type === 'text' && b.text) {
                  // If the text looks like extracted file content (very long, no
                  // punctuation pattern), summarize it as an attachment label.
                  if (b.text.startsWith('[System: the user attached') || b.text.length > 500) {
                    const match = b.text.match(/attached the file "(.+?)"/);
                    if (match) {
                      texts.push(`[Attached: ${match[1]}]`);
                    }
                  } else {
                    texts.push(b.text);
                  }
                } else if (b.type === 'image_url') {
                  texts.push('[Attached image]');
                }
              }
              content = texts.join('\n') || content;
            }
          } catch { /* not JSON, use as-is */ }
        }
        return {
          id: m.id,
          role: m.role as 'user' | 'assistant' | 'system',
          content,
          timestamp: new Date(m.created_at),
          toolsUsed: parseToolsUsed(m.tools_used),
        };
      });
  }, []);

  // Persist draft text across page navigations.
  useEffect(() => {
    sessionStorage.setItem('chat-draft', input);
  }, [input]);

  // Auto-focus input when the Chat page mounts.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);


  // Persist active session key so it survives page navigation.
  useEffect(() => {
    if (sessionKey) {
      sessionStorage.setItem('chat-active-session', sessionKey);
    }
  }, [sessionKey]);

  // Sync session key from hash changes (browser back/forward).
  useEffect(() => {
    const onHash = () => setSessionKey(parseSessionKey());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  // Fetch session list.
  const refreshSessions = useCallback(async () => {
    try {
      const list = await fetchJSON<Session[]>('/api/sessions');
      setSessions(list ?? []);
      // Clean up pendingPrivate entries that now exist server-side.
      setPendingPrivate((prev) => {
        if (prev.size === 0) return prev;
        const persisted = new Set((list ?? []).map((s) => s.key));
        const next = new Set<string>();
        for (const k of prev) {
          if (!persisted.has(k)) next.add(k);
        }
        return next.size === prev.size ? prev : next;
      });
      return list ?? [];
    } catch {
      return [];
    }
  }, []);
  const refreshSessionsRef = useRef(refreshSessions);
  refreshSessionsRef.current = refreshSessions;

  // On mount, load sessions and auto-select if no key in URL.
  useEffect(() => {
    refreshSessions().then((list) => {
      if (parseSessionKey()) return;
      // Restore persisted session if it still exists.
      const saved = sessionStorage.getItem('chat-active-session');
      if (saved && list.some((s) => s.key === saved)) {
        navigateToSession(saved);
        putJSON(`/api/sessions/${encodeURIComponent(saved)}/activate`, {}).catch(() => {});
        return;
      }
      // Prefer the already-active web session, else the most recent one.
      const active = list.find((s) => s.channel === 'web' && s.is_active);
      const webSession = active || list.find((s) => s.channel === 'web');
      if (webSession) {
        navigateToSession(webSession.key);
        putJSON(`/api/sessions/${encodeURIComponent(webSession.key)}/activate`, {}).catch(() => {});
      } else {
        // No sessions exist: auto-create one so the user lands in a ready chat.
        const chatId = Date.now().toString(36);
        navigateToSession(`web:${chatId}`);
      }
    });
  }, [refreshSessions]);

  // Load messages when session key changes.
  useEffect(() => {
    if (!sessionKey) {
      setMessages([]);
      return;
    }

    const gen = ++loadGenRef.current;
    setMessages([]);
    setActivity(null);

    fetchJSON<SessionDetail>(`/api/sessions/${encodeURIComponent(sessionKey)}`)
      .then((data) => {
        if (gen !== loadGenRef.current) return;
        setMessages(toChatMessages(data.messages ?? []));
      })
      .catch(() => {
        // Session doesn't exist yet; start fresh.
      });
  }, [sessionKey, toChatMessages]);

  // Scroll to bottom on new messages or activity changes.
  const scrollToBottom = useCallback(() => {
    const el = scrollContainerRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, activity, scrollToBottom]);

  // Register WS listener for incoming messages.
  const sessionKeyRef = useRef(sessionKey);
  sessionKeyRef.current = sessionKey;

  useEffect(() => {
    const listener = (data: WsMessage) => {
      if (data.type === 'status') {
        // Only show activity for the currently viewed session.
        if (data.session_key && data.session_key !== sessionKeyRef.current) return;
        setActivity({
          status: data.status as 'thinking' | 'tool_calling',
          tool: data.tool || undefined,
        });
        return;
      }

      if (data.type === 'session_title') {
        refreshSessionsRef.current();
        return;
      }

      if (data.type === 'session_deleted') {
        refreshSessionsRef.current();
        if (data.session_key && data.session_key === sessionKeyRef.current) {
          setMessages([]);
          setActivity(null);
        }
        return;
      }

      if (data.type === 'session_update') {
        refreshSessionsRef.current();
        // Re-fetch pinned session when tasks:output is created or updated.
        if (data.session_key === 'tasks:output') {
          fetchJSON<SessionDetail>('/api/sessions/tasks:output').then((d) => setPinnedSession(d.session)).catch(() => {});
        }
        if (data.session_key && data.session_key === sessionKeyRef.current) {
          fetchJSON<SessionDetail>(`/api/sessions/${encodeURIComponent(data.session_key)}`)
            .then((detail) => {
              if (!detail.messages) return;
              const next = toChatMessages(detail.messages);
              setMessages((prev) => {
                if (messagesFingerprint(prev) === messagesFingerprint(next)) return prev;
                return next;
              });
            })
            .catch(() => {});
        }
        if (data.session_key && data.session_key !== sessionKeyRef.current) {
          setUnreadSessions((prev) => new Set(prev).add(data.session_key!));
        }
        return;
      }

      if (data.type === 'settings_changed') {
        refreshSessionsRef.current();
        return;
      }

      if (data.type === 'cancelled') {
        // Only update UI for the currently viewed session.
        if (data.session_key && data.session_key !== sessionKeyRef.current) return;
        setActivity(null);
        // Reload messages from server to show the [cancelled] system note.
        if (data.session_key === sessionKeyRef.current) {
          fetchJSON<SessionDetail>(`/api/sessions/${encodeURIComponent(data.session_key)}`)
            .then((detail) => {
              if (!detail.messages) return;
              const next = toChatMessages(detail.messages);
              setMessages((prev) => {
                if (messagesFingerprint(prev) === messagesFingerprint(next)) return prev;
                return next;
              });
            })
            .catch(() => {});
        }
        return;
      }

      // Response or error: only apply to the currently viewed session.
      if (data.session_key && data.session_key !== sessionKeyRef.current) {
        // Not our session; trigger a sidebar refresh so unread indicator updates.
        refreshSessionsRef.current();
        return;
      }
      setActivity(null);

      if (data.type === 'error') {
        setMessages((prev) => [...prev, {
          role: 'assistant',
          content: `Error: ${data.error}`,
          timestamp: new Date(),
        }]);
        return;
      }

      if (data.type === 'response') {
        const content = data.content || data.message || '';
        let resolved: ResolvedTools | undefined;
        if (data.tools_used) {
          try {
            const parsed = typeof data.tools_used === 'string'
              ? JSON.parse(data.tools_used)
              : data.tools_used;
            if (parsed.skills?.length || parsed.tools?.length || parsed.memory?.length) {
              resolved = parsed;
            }
          } catch { /* ignore parse errors */ }
        }
        setMessages((prev) => [...prev, {
          role: 'assistant',
          content: String(content),
          timestamp: new Date(),
          toolsUsed: resolved,
        }]);

        // Session is now guaranteed to exist server-side; refresh the
        // sidebar so newly created sessions appear immediately.
        refreshSessionsRef.current();

        const preview = String(content).replace(/[#*_`>\[\]]/g, '').slice(0, 120);
        sendNotification('New response', preview);
      }
    };

    subscribe(listener);
    return () => { unsubscribe(listener); };
  }, [subscribe, unsubscribe, toChatMessages]);

  // Bind session key to the WS connection so the server knows where
  // to push task notifications.
  useEffect(() => {
    wsSetSessionKey(sessionKey);
  }, [sessionKey, wsSetSessionKey]);

  // Check if the current session is private.
  const isCurrentSessionPrivate = useMemo(() => {
    if (!sessionKey) return false;
    if (pendingPrivate.has(sessionKey)) return true;
    return sessions.find((s) => s.key === sessionKey)?.private === true;
  }, [sessionKey, pendingPrivate, sessions]);

  // Send an arbitrary text message on the current session.
  const sendText = useCallback(async (text: string) => {
    const key = sessionKeyRef.current;
    if ((!text && !selectedFile) || !key || !connected) return;

    const chatId = key.startsWith('web:') ? key.slice(4) : key;
    const isPrivate = pendingPrivate.has(key) || sessions.find((s) => s.key === key)?.private === true;

    const displayText = selectedFile ? (text ? `${text}\n[Attached: ${selectedFile.name}]` : `[Attached: ${selectedFile.name}]`) : text;
    setMessages((prev) => [...prev, {
      role: 'user',
      content: displayText,
      timestamp: new Date(),
    }]);
    setInput('');
    if (inputRef.current) inputRef.current.style.height = 'auto';
    const file = selectedFile;
    setSelectedFile(null);
    setActivity({ status: 'thinking' });

    if (file) {
      try {
        const resp = await sendChatMessage(text, key, chatId, file, isPrivate);
        setActivity(null);
        if (resp.content) {
          let resolved: ResolvedTools | undefined;
          if (resp.tools_used) {
            const tu = resp.tools_used as ResolvedTools;
            if (tu.skills?.length || tu.tools?.length || tu.memory?.length) {
              resolved = tu;
            }
          }
          setMessages((prev) => [...prev, {
            role: 'assistant',
            content: resp.content,
            timestamp: new Date(),
            toolsUsed: resolved,
          }]);
        }
      } catch (e) {
        setActivity(null);
        console.error('upload failed', e);
        setMessages((prev) => [...prev, {
          role: 'assistant',
          content: 'Error: file upload failed',
          timestamp: new Date(),
        }]);
      }
    } else {
      wsSend({
        type: 'message',
        message: text,
        session_key: key,
        chat_id: chatId,
        ...(isPrivate && { private: true }),
      });
    }

    sessionStorage.removeItem('chat-draft');
    inputRef.current?.focus();
    refreshSessions();
  }, [connected, wsSend, refreshSessions, pendingPrivate, sessions, selectedFile]);

  const cancelRequest = useCallback(() => {
    const key = sessionKeyRef.current;
    if (!key) return;
    setActivity(null);
    wsSend({ type: 'cancel', session_key: key });
  }, [wsSend]);

  // Send the current input field value.
  const send = useCallback(() => {
    const text = input.trim();
    if (text || selectedFile) sendText(text);
  }, [input, sendText, selectedFile]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  };

  // Session actions.
  const handleNewSession = useCallback((isPrivate?: boolean) => {
    const chatId = Date.now().toString(36);
    const key = `web:${chatId}`;
    if (isPrivate) {
      setPendingPrivate((prev) => new Set(prev).add(key));
    }
    navigateToSession(key);
    // The session doesn't exist yet; it'll be activated on first message.
    setTimeout(() => inputRef.current?.focus(), 0);
  }, []);

  const handleSelectSession = useCallback((key: string) => {
    navigateToSession(key);
    putJSON(`/api/sessions/${encodeURIComponent(key)}/activate`, {}).catch(() => {});
    setUnreadSessions((prev) => {
      if (!prev.has(key)) return prev;
      const next = new Set(prev);
      next.delete(key);
      return next;
    });
    if (key === 'tasks:output') {
      markAllNotificationsRead().catch(() => {});
    }
  }, []);

  const handleDeleteSession = useCallback(async (key: string) => {
    try {
      await deleteJSON(`/api/sessions/${encodeURIComponent(key)}`);
    } catch {
      return;
    }
    await refreshSessions();
    if (key === sessionKeyRef.current) {
      handleNewSession();
    }
  }, [refreshSessions]);

  const handleDeleteMessage = useCallback(async (msgId: number) => {
    const key = sessionKeyRef.current;
    if (!key) return;
    try {
      await deleteJSON(`/api/sessions/${encodeURIComponent(key)}/messages/${msgId}`);
      setMessages((prev) => prev.filter((m) => m.id !== msgId));
    } catch { /* ignore */ }
  }, []);

  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed((prev) => {
      localStorage.setItem('chat-sidebar-collapsed', String(!prev));
      return !prev;
    });
  }, []);

  return (
    <div className="flex flex-col h-full">
      <PageHeader title="Chat" subtitle="Real-time conversation" />

      <div className="flex flex-1 min-h-0">
        <SessionSidebar
          sessions={sessions}
          activeKey={sessionKey}
          onSelect={handleSelectSession}
          onNew={handleNewSession}
          onDelete={handleDeleteSession}
          collapsed={sidebarCollapsed}
          onToggle={toggleSidebar}
          pinnedSession={pinnedSession}
          unreadSessions={unreadSessions}
        />

        <div className="flex flex-col flex-1 min-h-0 min-w-0 pl-4">
          {/* Disconnected banner */}
          {!connected && (
            <div className="flex items-center gap-2 mb-2">
              <span className={`w-2 h-2 rounded-full ${connecting ? 'bg-orange-500 animate-pulse' : 'bg-zinc-600'}`} />
              <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500">
                {connecting ? 'Connecting' : 'Disconnected'}
              </span>
              {!connecting && (
                <button
                  onClick={connect}
                  className="text-[13px] uppercase tracking-[0.2em] font-medium text-orange-500 hover:text-orange-400 cursor-pointer ml-2"
                >
                  Reconnect
                </button>
              )}
            </div>
          )}

          {/* Messages */}
          {sessionKey ? (
            <Panel className="flex-1 flex flex-col min-h-0 min-w-0 !p-0">
              {isCurrentSessionPrivate && (
                <div className="flex items-center gap-2 px-4 py-1.5 border-b border-zinc-800/60">
                  <span className="flex items-center gap-1 text-[11px] uppercase tracking-[0.2em] font-medium text-amber-500">
                    <Lock size={10} />
                    Private
                  </span>
                </div>
              )}
              <div ref={scrollContainerRef} className="flex-1 overflow-y-auto overflow-x-hidden p-5">
                {messages.length === 0 && !activity ? (
                  sessionKey === 'tasks:output' ? (
                  <div className="flex flex-col items-center justify-center h-full gap-4">
                    <ListTodo size={32} className="text-zinc-700" />
                    <div className="text-center">
                      <p className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500 mb-2">
                        Task Output
                      </p>
                      <p className="text-sm text-zinc-600 max-w-xs">
                        Results from completed tasks will appear here. Enable "Output to chat" on a task to see its results.
                      </p>
                    </div>
                  </div>
                  ) : (
                  <div className="flex flex-col items-center justify-center h-full gap-6">
                    <div className="text-center">
                      <p className="text-[13px] uppercase tracking-[0.2em] font-medium text-orange-500 mb-2">
                        Cogitator
                      </p>
                      <p className="text-base text-zinc-500">
                        Your personal AI assistant that learns and adapts.
                      </p>
                    </div>
                    <div className="flex flex-wrap justify-center gap-2 max-w-lg">
                      {SUGGESTIONS.map((s) => {
                        const isIntro = s === SUGGESTIONS[0];
                        const strong = isIntro && hasMemories === false;
                        const subtle = isIntro && !strong;
                        return (
                          <button
                            key={s}
                            onClick={() => sendText(s)}
                            disabled={!connected}
                            className={`px-3 py-1.5 text-sm transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-default ${
                              strong
                                ? 'text-orange-400 border border-orange-600/60 bg-orange-950/30 hover:bg-orange-950/50 hover:border-orange-600'
                                : subtle
                                  ? 'text-zinc-300 border border-zinc-600 hover:border-orange-600/50 hover:text-orange-400'
                                  : 'text-zinc-400 border border-zinc-700 hover:border-orange-600/50 hover:text-orange-400'
                            }`}
                          >
                            {s}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                  )
                ) : (
                  <div className="space-y-3 min-w-0">
                    {messages.map((msg, i) => (
                      <MessageBubble key={msg.id ?? i} message={msg} onDelete={msg.id ? handleDeleteMessage : undefined} />
                    ))}
                    {activity && <ActivityIndicator activity={activity} />}
                    <div ref={messagesEndRef} />
                  </div>
                )}
              </div>
            </Panel>
          ) : (
            <Panel className="flex-1 flex items-center justify-center min-h-0">
              <div className="text-center">
                <p className="text-base text-zinc-500 mb-3">Select a session or start a new conversation.</p>
                <button
                  onClick={() => handleNewSession()}
                  className="text-[12px] uppercase tracking-widest font-medium text-orange-500 hover:text-orange-400 cursor-pointer"
                >
                  New Chat
                </button>
              </div>
            </Panel>
          )}

          {sessionKey !== 'tasks:output' && (
            <>
              {/* File preview */}
              {selectedFile && (
                <div className="flex items-center gap-2 px-3 py-1.5 bg-zinc-800 border border-zinc-700 text-zinc-300 text-sm">
                  <span className="truncate max-w-[200px]">{selectedFile.name}</span>
                  <span className="text-zinc-500">({(selectedFile.size / 1024).toFixed(0)} KB)</span>
                  <button onClick={() => setSelectedFile(null)} className="text-zinc-500 hover:text-zinc-300 ml-auto">x</button>
                </div>
              )}

              {/* Input */}
              <input
                ref={fileInputRef}
                type="file"
                accept={ACCEPTED_FILE_TYPES}
                className="absolute w-0 h-0 overflow-hidden"
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) {
                    if (file.size > MAX_FILE_SIZE) {
                      alert('File must be under 10MB');
                      return;
                    }
                    setSelectedFile(file);
                  }
                  e.target.value = '';
                }}
              />
              <div className="mt-4 bg-zinc-900 border border-zinc-700 overflow-hidden focus-within:border-orange-600 focus-within:ring-1 focus-within:ring-orange-600/20 transition-colors">
                <textarea
                  ref={inputRef}
                  value={input}
                  onChange={(e) => {
                    setInput(e.target.value);
                    const el = e.target;
                    el.style.height = 'auto';
                    el.style.height = Math.min(el.scrollHeight, 160) + 'px';
                  }}
                  onKeyDown={handleKeyDown}
                  rows={1}
                  placeholder={!sessionKey ? 'Select or create a session...' : connected ? 'Type a message...' : 'Waiting for connection...'}
                  disabled={!connected || !sessionKey}
                  className="w-full bg-transparent px-4 pt-3 pb-2 text-zinc-100 text-base resize-none focus:outline-none disabled:opacity-50 placeholder:text-zinc-500"
                  style={{ minHeight: '40px', maxHeight: '160px' }}
                />
                <div className="flex items-center justify-between px-2 pb-2">
                  <button
                    onClick={() => fileInputRef.current?.click()}
                    className="p-2 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800 cursor-pointer transition-colors"
                    title="Attach file"
                  >
                    <Paperclip size={18} />
                  </button>
                  {activity ? (
                    <button
                      onClick={cancelRequest}
                      className="px-4 py-1.5 bg-red-900/40 border border-red-600/50 text-red-400 hover:bg-red-900/60 hover:text-red-300 text-[12px] uppercase tracking-widest font-medium cursor-pointer transition-colors"
                    >
                      Stop
                    </button>
                  ) : (
                    <button
                      onClick={send}
                      disabled={!connected || !sessionKey || (!input.trim() && !selectedFile)}
                      className="px-4 py-1.5 bg-orange-600 text-white text-[12px] uppercase tracking-widest font-medium hover:bg-orange-500 disabled:bg-zinc-800 disabled:text-zinc-600 cursor-pointer disabled:cursor-default transition-colors"
                    >
                      Send
                    </button>
                  )}
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

const THINKING_LABELS = [
  'Thinking',
  'Working',
  'Processing',
  'Reasoning',
  'Analyzing',
  'Considering',
  'Reflecting',
  'Composing',
];

function ActivityIndicator({ activity }: { activity: Activity }) {
  const [labelIndex, setLabelIndex] = useState(0);

  useEffect(() => {
    setLabelIndex(0);
    if (activity.status !== 'thinking') return;
    const id = setInterval(() => {
      setLabelIndex((i) => (i + 1) % THINKING_LABELS.length);
    }, 3500);
    return () => clearInterval(id);
  }, [activity.status, activity.tool]);

  let label: string;
  if (activity.status === 'tool_calling' && activity.tool) {
    label = TOOL_LABELS[activity.tool] || `Using ${activity.tool}`;
  } else {
    label = THINKING_LABELS[labelIndex];
  }

  return (
    <div className="flex justify-start">
      <div className="flex items-center gap-2.5 px-3 py-2">
        <Spinner />
        <span className="text-[13px] uppercase tracking-[0.2em] font-medium text-zinc-500">
          {label}
        </span>
      </div>
    </div>
  );
}

function Spinner() {
  return (
    <svg
      className="animate-spin h-3.5 w-3.5 text-orange-500"
      viewBox="0 0 24 24"
      fill="none"
    >
      <circle
        className="opacity-20"
        cx="12" cy="12" r="10"
        stroke="currentColor"
        strokeWidth="3"
      />
      <path
        className="opacity-80"
        d="M12 2a10 10 0 0 1 10 10"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
    </svg>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const copy = () => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <button
      onClick={copy}
      className="opacity-0 group-hover:opacity-100 transition-opacity text-zinc-600 hover:text-zinc-400 cursor-pointer ml-auto"
      title="Copy to clipboard"
    >
      {copied ? (
        <svg className="w-3.5 h-3.5 text-green-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="20 6 9 17 4 12" />
        </svg>
      ) : (
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
        </svg>
      )}
    </button>
  );
}

function DeleteButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="opacity-0 group-hover:opacity-100 transition-opacity text-zinc-600 hover:text-red-400 cursor-pointer"
      title="Delete message"
    >
      <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <polyline points="3 6 5 6 21 6" />
        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      </svg>
    </button>
  );
}

function ToolsUsedIndicator({ toolsUsed }: { toolsUsed: ResolvedTools }) {
  const [expanded, setExpanded] = useState(false);
  const { skills, tools, memory } = toolsUsed;

  const parts: string[] = [];
  if (skills?.length) parts.push(`${skills.length} skill${skills.length > 1 ? 's' : ''}`);
  if (tools?.length) parts.push(`${tools.length} tool${tools.length > 1 ? 's' : ''}`);
  if (memory?.length) parts.push('memory');
  const summary = parts.join(', ');
  if (!summary) return null;

  return (
    <div className="mt-1.5">
      <button
        onClick={() => setExpanded((v) => !v)}
        className="flex items-center gap-1 text-[11px] text-zinc-600 hover:text-zinc-400 transition-colors cursor-pointer"
      >
        <svg
          className={`w-2.5 h-2.5 transition-transform ${expanded ? 'rotate-90' : ''}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="3"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <polyline points="9 18 15 12 9 6" />
        </svg>
        <span>{summary}</span>
      </button>
      {expanded && (
        <div className="ml-3.5 mt-1 space-y-0.5 text-[11px]">
          {skills && skills.length > 0 && (
            <div>
              <span className="text-zinc-600 uppercase tracking-widest">Skills: </span>
              <span className="text-zinc-500">{skills.join(', ')}</span>
            </div>
          )}
          {tools && tools.length > 0 && (
            <div>
              <span className="text-zinc-600 uppercase tracking-widest">Tools: </span>
              <span className="text-zinc-500">{tools.join(', ')}</span>
            </div>
          )}
          {memory && memory.length > 0 && (
            <div>
              <span className="text-zinc-600 uppercase tracking-widest">Memory: </span>
              <span className="text-zinc-500">{memory.join(', ')}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

const MessageBubble = memo(function MessageBubble({ message, onDelete }: { message: ChatMessage; onDelete?: (id: number) => void }) {
  const time = message.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  if (message.role === 'system' && message.content === '[cancelled]') {
    return (
      <div className="flex justify-center py-1">
        <span className="text-[11px] uppercase tracking-widest text-zinc-600">
          Cancelled
        </span>
      </div>
    );
  }

  if (message.role === 'system') {
    return (
      <div className="flex justify-start group">
        <div className="max-w-[80%] bg-zinc-800/30 border border-zinc-700 p-3">
          <div className="flex items-center gap-2 mb-1">
            <span className="text-[12px] uppercase tracking-widest font-medium text-blue-400">Task Result</span>
            <span className="text-[12px] text-zinc-600">{time}</span>
            <CopyButton text={message.content} />
            {onDelete && message.id && <DeleteButton onClick={() => onDelete(message.id!)} />}
          </div>
          <div className="prose-chat break-words">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{message.content}</ReactMarkdown>
          </div>
        </div>
      </div>
    );
  }

  const isUser = message.role === 'user';

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'} group`}>
      <div className={`max-w-[80%] ${
        isUser
          ? 'bg-orange-900/20 border border-orange-600/30'
          : 'bg-zinc-800/50 border border-zinc-700'
      } p-3`}>
        <div className="flex items-center gap-2 mb-1">
          <span className={`text-[12px] uppercase tracking-widest font-medium ${
            isUser ? 'text-orange-500' : 'text-zinc-500'
          }`}>
            {isUser ? 'You' : 'Cogitator'}
          </span>
          <span className="text-[12px] text-zinc-600">{time}</span>
          <CopyButton text={message.content} />
          {onDelete && message.id && <DeleteButton onClick={() => onDelete(message.id!)} />}
        </div>
        {isUser ? (
          <p className="text-base text-zinc-300 whitespace-pre-wrap break-words">{message.content}</p>
        ) : (
          <>
            <div className="prose-chat break-words">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{message.content}</ReactMarkdown>
            </div>
            {message.toolsUsed && (
              <ToolsUsedIndicator toolsUsed={message.toolsUsed} />
            )}
          </>
        )}
      </div>
    </div>
  );
});

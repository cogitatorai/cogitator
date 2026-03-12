import { createContext, useContext, useEffect, useRef, useState, useCallback } from 'react';
import { authHeaders } from './api';

// Raw parsed message from the WebSocket.
export interface WsMessage {
  type: string;
  message?: string;
  session_key?: string;
  chat_id?: string;
  content?: string;
  error?: string;
  status?: string;
  tool?: string;
  [key: string]: unknown;
}

export type WsListener = (msg: WsMessage) => void;

interface WebSocketContextValue {
  connected: boolean;
  connecting: boolean;
  connect: () => void;
  send: (msg: Record<string, unknown>) => void;
  setSessionKey: (key: string | null) => void;
  subscribe: (listener: WsListener) => void;
  unsubscribe: (listener: WsListener) => void;
}

const WebSocketContext = createContext<WebSocketContextValue | null>(null);

function wsUrl(): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const base = `${proto}//${window.location.host}/ws`;
  // Extract the Bearer token from authHeaders (reads from the JWT token provider).
  const bearer = authHeaders()['Authorization'];
  const token = bearer?.replace('Bearer ', '');
  return token ? `${base}?token=${encodeURIComponent(token)}` : base;
}

const RECONNECT_MIN = 1000;
const RECONNECT_MAX = 10000;

export function WebSocketProvider({ children }: { children: React.ReactNode }) {
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);

  const wsRef = useRef<WebSocket | null>(null);
  const listenersRef = useRef<Set<WsListener>>(new Set());
  const sessionKeyRef = useRef<string | null>(null);
  const backoffRef = useRef(RECONNECT_MIN);
  const intentionalCloseRef = useRef(false);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const dispatch = useCallback((msg: WsMessage) => {
    listenersRef.current.forEach((fn) => {
      try { fn(msg); } catch { /* listener errors don't break the provider */ }
    });
  }, []);

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) return;
    if (wsRef.current?.readyState === WebSocket.CONNECTING) return;

    intentionalCloseRef.current = false;
    setConnecting(true);

    const ws = new WebSocket(wsUrl());
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      setConnecting(false);
      backoffRef.current = RECONNECT_MIN;

      // Re-subscribe to the current session if one was set before reconnect.
      if (sessionKeyRef.current) {
        ws.send(JSON.stringify({ type: 'subscribe', session_key: sessionKeyRef.current }));
      }
    };

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as WsMessage;
        dispatch(data);
      } catch {
        dispatch({ type: 'raw', content: event.data });
      }
    };

    ws.onclose = () => {
      setConnected(false);
      setConnecting(false);
      wsRef.current = null;

      if (!intentionalCloseRef.current) {
        const delay = backoffRef.current;
        backoffRef.current = Math.min(delay * 1.5, RECONNECT_MAX);
        reconnectTimerRef.current = setTimeout(() => {
          reconnectTimerRef.current = null;
          connect();
        }, delay);
      }
    };

    ws.onerror = () => {
      setConnecting(false);
    };
  }, [dispatch]);

  // Connect on mount, clean up on unmount.
  useEffect(() => {
    connect();
    return () => {
      intentionalCloseRef.current = true;
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      wsRef.current?.close();
    };
  }, [connect]);

  const send = useCallback((msg: Record<string, unknown>) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify(msg));
  }, []);

  const setSessionKey = useCallback((key: string | null) => {
    sessionKeyRef.current = key;
    if (key && wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'subscribe', session_key: key }));
    }
  }, []);

  const subscribe = useCallback((listener: WsListener) => {
    listenersRef.current.add(listener);
  }, []);

  const unsubscribe = useCallback((listener: WsListener) => {
    listenersRef.current.delete(listener);
  }, []);

  const value: WebSocketContextValue = {
    connected,
    connecting,
    connect,
    send,
    setSessionKey,
    subscribe,
    unsubscribe,
  };

  return (
    <WebSocketContext.Provider value={value}>
      {children}
    </WebSocketContext.Provider>
  );
}

export function useWebSocket(): WebSocketContextValue {
  const ctx = useContext(WebSocketContext);
  if (!ctx) throw new Error('useWebSocket must be used within WebSocketProvider');
  return ctx;
}

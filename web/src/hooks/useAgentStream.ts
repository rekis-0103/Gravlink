import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { TranscriptStep } from '../types';

export type StreamState = 'idle' | 'connecting' | 'live' | 'polling' | 'reconnecting' | 'error';

interface WsPayload {
  type: 'snapshot' | 'delta' | 'error';
  steps?: TranscriptStep[];
  error?: string;
}

export function useAgentStream(conversationId: string | null) {
  const [steps, setSteps] = useState<TranscriptStep[]>([]);
  const [streamState, setStreamState] = useState<StreamState>('idle');
  const retryTimer = useRef<number | null>(null);
  const pollTimer = useRef<number | null>(null);

  useEffect(() => {
    if (!conversationId) {
      setSteps([]);
      setStreamState('idle');
      return;
    }

    let cancelled = false;
    let ws: WebSocket | null = null;
    let attempt = 0;
    let wsOpened = false;

    const clearTimers = () => {
      if (retryTimer.current !== null) {
        window.clearTimeout(retryTimer.current);
        retryTimer.current = null;
      }
      if (pollTimer.current !== null) {
        window.clearInterval(pollTimer.current);
        pollTimer.current = null;
      }
    };

    const startPollingFallback = async () => {
      if (cancelled || pollTimer.current !== null) return;
      setStreamState('polling');

      const fetchSteps = async () => {
        if (cancelled) return;
        try {
          const res = await api.steps(conversationId);
          if (!cancelled) {
            setSteps(res.steps ?? []);
            setStreamState('polling');
          }
        } catch {
          if (!cancelled) setStreamState('error');
        }
      };

      await fetchSteps();
      if (!cancelled) {
        pollTimer.current = window.setInterval(() => {
          void fetchSteps();
        }, 1500);
      }
    };

    const connect = () => {
      if (cancelled) return;
      setStreamState(attempt === 0 ? 'connecting' : 'reconnecting');

      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = `${proto}//${window.location.host}/api/ws/conversations/${encodeURIComponent(conversationId)}`;

      try {
        ws = new WebSocket(url);
      } catch {
        void startPollingFallback();
        return;
      }

      ws.onopen = () => {
        if (cancelled) return;
        wsOpened = true;
        attempt = 0;
        clearTimers();
        setStreamState('live');
      };

      ws.onmessage = (ev) => {
        if (cancelled) return;
        try {
          const msg = JSON.parse(ev.data) as WsPayload;
          if (msg.type === 'snapshot') {
            setSteps(msg.steps ?? []);
          } else if (msg.type === 'delta' && msg.steps && msg.steps.length > 0) {
            setSteps((prev) => {
              const map = new Map<number, TranscriptStep>();
              for (const s of prev) map.set(s.step_index, s);
              for (const s of msg.steps ?? []) map.set(s.step_index, s);
              return Array.from(map.values()).sort((a, b) => a.step_index - b.step_index);
            });
          }
        } catch {
          // Ignore malformed frame
        }
      };

      ws.onclose = () => {
        if (cancelled) return;
        attempt += 1;
        // If WebSocket never opened after 2 attempts (e.g. iOS Safari rejecting untrusted wss:// cert),
        // fall back cleanly to HTTPS polling so updates continue live.
        if (!wsOpened && attempt >= 2) {
          void startPollingFallback();
          return;
        }
        setStreamState('reconnecting');
        const delay = Math.min(5000, 500 * Math.pow(1.5, attempt));
        retryTimer.current = window.setTimeout(connect, delay);
      };

      ws.onerror = () => {
        if (cancelled) return;
        if (!wsOpened && attempt >= 1) {
          ws?.close();
        }
      };
    };

    setSteps([]);
    connect();

    return () => {
      cancelled = true;
      clearTimers();
      ws?.close();
    };
  }, [conversationId]);

  return { steps, streamState };
}

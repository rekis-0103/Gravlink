import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import { isActiveStepStatus } from '../lib/agentStatus';
import type { TranscriptStep } from '../types';

export type StreamState = 'idle' | 'connecting' | 'live' | 'polling' | 'reconnecting' | 'error';

interface WsPayload {
  type: 'snapshot' | 'delta' | 'error';
  steps?: TranscriptStep[];
  error?: string;
}

function mergeDeltaSteps(prev: TranscriptStep[], incoming: TranscriptStep[]): TranscriptStep[] {
  if (incoming.length === 0) return prev;
  const map = new Map<number, TranscriptStep>();
  for (const s of prev) map.set(s.step_index, s);
  let changed = false;
  for (const s of incoming) {
    const old = map.get(s.step_index);
    if (
      !old ||
      old.status !== s.status ||
      old.content !== s.content ||
      old.thinking !== s.thinking ||
      (old.tool_calls?.length ?? 0) !== (s.tool_calls?.length ?? 0) ||
      old.output_tokens !== s.output_tokens
    ) {
      changed = true;
    }
    map.set(s.step_index, s);
  }
  if (!changed) return prev;
  return Array.from(map.values()).sort((a, b) => a.step_index - b.step_index);
}

function advanceCursor(steps: TranscriptStep[], currentCursor: number): number {
  let cursor = currentCursor;
  for (let i = 0; i < steps.length; i++) {
    const s = steps[i];
    const isLast = i === steps.length - 1;
    if (isLast && isActiveStepStatus(s.status)) {
      break;
    }
    if (s.step_index >= cursor) {
      cursor = s.step_index + 1;
    }
  }
  return cursor;
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
    let pollCursor = 0;

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

      const fetchSteps = async (initial: boolean) => {
        if (cancelled || (!initial && document.hidden)) return;
        try {
          const fromIdx = initial ? 0 : pollCursor;
          const res = await api.steps(conversationId, fromIdx);
          if (cancelled) return;
          const incoming = res.steps ?? [];
          if (initial) {
            setSteps(incoming);
            pollCursor = advanceCursor(incoming, 0);
          } else if (incoming.length > 0) {
            setSteps((prev) => mergeDeltaSteps(prev, incoming));
            pollCursor = advanceCursor(incoming, pollCursor);
          }
          setStreamState('polling');
        } catch {
          if (!cancelled) setStreamState('error');
        }
      };

      await fetchSteps(true);
      if (!cancelled) {
        pollTimer.current = window.setInterval(() => {
          void fetchSteps(false);
        }, 1600);
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
            setSteps((prev) => mergeDeltaSteps(prev, msg.steps ?? []));
          }
        } catch {
          // Ignore malformed frame
        }
      };

      ws.onclose = () => {
        if (cancelled) return;
        attempt += 1;
        if (!wsOpened && attempt >= 2) {
          void startPollingFallback();
          return;
        }
        setStreamState('reconnecting');
        const delay = Math.min(5000, 500 * Math.pow(1.5, attempt));
        retryTimer.current = window.setTimeout(connect, delay);
      };

      ws.onerror = () => {
        ws?.close();
      };
    };

    connect();

    return () => {
      cancelled = true;
      clearTimers();
      if (ws && ws.readyState < WebSocket.CLOSING) {
        ws.close();
      }
    };
  }, [conversationId]);

  return { steps, streamState };
}

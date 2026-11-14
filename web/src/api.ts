import type {
  AuthStatus,
  ConversationSummary,
  DoctorCheck,
  FileContent,
  ModelOption,
  PendingResponse,
  PromptResponse,
  SystemStatus,
  TranscriptStep,
  TreeNode,
} from './types';

let csrfToken = '';

export function setCSRFToken(token: string) {
  csrfToken = token;
}

async function request<T>(url: string, init: RequestInit = {}, timeoutMs = 10000): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.method && init.method !== 'GET' && init.method !== 'HEAD') {
    headers.set('Content-Type', 'application/json');
    if (csrfToken) {
      headers.set('X-Gravlink-CSRF', csrfToken);
    }
  }

  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), timeoutMs);

  try {
    const res = await fetch(url, {
      ...init,
      headers,
      credentials: 'same-origin',
      signal: init.signal ?? controller.signal,
    });

    if (!res.ok) {
      const msg = await res.text();
      throw new Error(msg.trim() || `HTTP ${res.status}`);
    }
    return (await res.json()) as T;
  } finally {
    window.clearTimeout(timeout);
  }
}

export const api = {
  authStatus: () => request<AuthStatus>('/api/auth/status'),
  login: (password: string) =>
    request<AuthStatus>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    }),
  pair: (token: string) =>
    request<AuthStatus>('/api/auth/pair', {
      method: 'POST',
      body: JSON.stringify({ token }),
    }),
  logout: () => request<{ ok: boolean }>('/api/auth/logout', { method: 'POST' }),
  status: () => request<SystemStatus>('/api/status'),
  doctor: () => request<{ checks: DoctorCheck[] }>('/api/doctor'),
  conversations: () => request<{ conversations: ConversationSummary[] }>('/api/conversations'),
  steps: (convId: string) =>
    request<{ steps: TranscriptStep[] }>(`/api/conversations/${encodeURIComponent(convId)}/steps`),
  tree: (relPath = '', depth = 4) =>
    request<{ root: string; nodes: TreeNode[] }>(
      `/api/workspace/tree?path=${encodeURIComponent(relPath)}&depth=${depth}`
    ),
  file: (relPath: string) =>
    request<FileContent>(`/api/workspace/file?path=${encodeURIComponent(relPath)}`),
  models: () => request<{ models: ModelOption[] }>('/api/models'),
  sendPrompt: (
    convId: string | 'new',
    payload: { prompt: string; mode?: string; model?: string }
  ) =>
    request<PromptResponse>(
      `/api/conversations/${encodeURIComponent(convId || 'new')}/prompt`,
      {
        method: 'POST',
        body: JSON.stringify(payload),
      },
      30000
    ),
  pending: (convId: string) =>
    request<PendingResponse>(`/api/conversations/${encodeURIComponent(convId)}/pending`),
  interact: (
    convId: string,
    payload: {
      trajectory_id: string;
      step_index: number;
      kind: string;
      allow: boolean;
      command_line?: string;
    }
  ) =>
    request<{ ok: boolean }>(
      `/api/conversations/${encodeURIComponent(convId)}/interact`,
      {
        method: 'POST',
        body: JSON.stringify(payload),
      },
      12000
    ),
};

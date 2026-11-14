import { useEffect, useRef, useState } from 'react';
import { Activity, Code2, LogOut, Menu, MessageSquare, RefreshCw, X } from 'lucide-react';
import { api, setCSRFToken } from './api';
import { AgentTimeline } from './components/AgentTimeline';
import { CodeExplorer } from './components/CodeExplorer';
import { DiagnosticsView } from './components/DiagnosticsView';
import { LoginView } from './components/LoginView';
import { useAgentStream } from './hooks/useAgentStream';
import type { ConversationSummary } from './types';

type Tab = 'agent' | 'code' | 'status';

export function App() {
  const [authChecked, setAuthChecked] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [tlsEnabled, setTlsEnabled] = useState(true);
  const [tab, setTab] = useState<Tab>('agent');
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [activeConvId, setActiveConvId] = useState<string | null>(null);
  const drawerRef = useRef<HTMLDialogElement>(null);

  const loadConversations = async () => {
    try {
      const res = await api.conversations();
      const list = res.conversations ?? [];
      setConversations(list);
      if (list.length > 0 && (!activeConvId || !list.some((c) => c.id === activeConvId))) {
        setActiveConvId(list[0].id);
      }
    } catch {
      // Handled gracefully in UI
    }
  };

  useEffect(() => {
    const boot = async () => {
      const params = new URLSearchParams(window.location.search);
      const pairToken = params.get('pair');
      if (pairToken) {
        try {
          const res = await api.pair(pairToken);
          if (res.csrf_token) setCSRFToken(res.csrf_token);
          setAuthenticated(true);
          window.history.replaceState({}, '', window.location.pathname);
        } catch {
          // Fall back to regular auth check / login form
        }
      }

      try {
        const st = await api.authStatus();
        setTlsEnabled(st.tls);
        setAuthenticated(st.authenticated);
        if (st.csrf_token) setCSRFToken(st.csrf_token);
      } finally {
        setAuthChecked(true);
      }
    };
    void boot();
  }, []);

  useEffect(() => {
    if (authenticated) {
      void loadConversations();
    }
  }, [authenticated]);

  const { steps, streamState } = useAgentStream(authenticated ? activeConvId : null);

  if (!authChecked) {
    return (
      <main className="login-page">
        <p style={{ color: 'var(--muted)' }}>Checking LAN session...</p>
      </main>
    );
  }

  if (!authenticated) {
    return <LoginView tlsEnabled={tlsEnabled} onSuccess={() => setAuthenticated(true)} />;
  }

  const activeConv = conversations.find((c) => c.id === activeConvId);
  const isLive = streamState === 'live' || streamState === 'polling';

  const openDrawer = () => {
    void loadConversations();
    drawerRef.current?.showModal();
  };

  const closeDrawer = () => {
    drawerRef.current?.close();
  };

  const handleLogout = async () => {
    try {
      await api.logout();
    } finally {
      setAuthenticated(false);
      setCSRFToken('');
    }
  };

  return (
    <div className="app-shell">
      <header className="top-bar">
        <button type="button" className="ghost" onClick={openDrawer} aria-label="Open sessions menu">
          <Menu size={20} aria-hidden="true" />
        </button>

        <div className="top-bar-title">
          <span className="session-label">{activeConv?.title || 'Gravlink'}</span>
          {activeConv && (
            <span className="session-sub">
              {activeConv.step_count} steps · {activeConv.last_status}
            </span>
          )}
        </div>

        <div className="status-pill" aria-live="polite">
          <span
            className={`dot ${isLive ? 'live' : streamState === 'error' ? 'danger' : 'warn'}`}
            aria-hidden="true"
          />
          <span>{streamState === 'live' ? 'LIVE' : streamState === 'polling' ? 'SYNC' : streamState.toUpperCase()}</span>
        </div>
      </header>

      <main className="main-area">
        {tab === 'agent' && (
          <AgentTimeline
            steps={steps}
            streamState={streamState}
            conversationTitle={activeConv?.title || 'No session selected'}
          />
        )}
        {tab === 'code' && <CodeExplorer />}
        {tab === 'status' && <DiagnosticsView />}
      </main>

      <nav className="bottom-nav" aria-label="Primary navigation">
        <button
          type="button"
          className={tab === 'agent' ? 'active' : ''}
          onClick={() => setTab('agent')}
        >
          <MessageSquare size={20} aria-hidden="true" />
          <span>Agent</span>
        </button>
        <button
          type="button"
          className={tab === 'code' ? 'active' : ''}
          onClick={() => setTab('code')}
        >
          <Code2 size={20} aria-hidden="true" />
          <span>Code</span>
        </button>
        <button
          type="button"
          className={tab === 'status' ? 'active' : ''}
          onClick={() => setTab('status')}
        >
          <Activity size={20} aria-hidden="true" />
          <span>Doctor</span>
        </button>
      </nav>

      <dialog
        ref={drawerRef}
        className="Drawer"
        aria-label="Antigravity sessions"
        onClick={(e) => {
          if (e.target === drawerRef.current) closeDrawer();
        }}
      >
        <div className="Drawer-sheet">
          <div className="Drawer-header">
            <strong style={{ fontSize: '0.95rem' }}>Antigravity Sessions</strong>
            <div style={{ display: 'flex', gap: '0.3rem' }}>
              <button type="button" className="ghost" onClick={() => void loadConversations()} aria-label="Refresh sessions">
                <RefreshCw size={17} aria-hidden="true" />
              </button>
              <button type="button" className="ghost" onClick={closeDrawer} aria-label="Close sessions menu">
                <X size={18} aria-hidden="true" />
              </button>
            </div>
          </div>

          <div className="Drawer-list">
            {conversations.length === 0 ? (
              <p style={{ color: 'var(--muted)', fontSize: '0.85rem', padding: '0.5rem' }}>
                No Antigravity conversations found yet.
              </p>
            ) : (
              conversations.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={`conv-item ${c.id === activeConvId ? 'active' : ''}`}
                  onClick={() => {
                    setActiveConvId(c.id);
                    setTab('agent');
                    closeDrawer();
                  }}
                >
                  <span className="conv-title">{c.title}</span>
                  <span className="conv-sub">
                    {c.step_count} steps · {c.last_status} · {new Date(c.last_step_at).toLocaleTimeString()}
                  </span>
                </button>
              ))
            )}
          </div>

          <div style={{ padding: '0.75rem', borderTop: '1px solid var(--border)' }}>
            <button type="button" style={{ width: '100%' }} onClick={handleLogout}>
              <LogOut size={16} aria-hidden="true" />
              Sign Out
            </button>
          </div>
        </div>
      </dialog>
    </div>
  );
}

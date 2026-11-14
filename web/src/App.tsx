import { useEffect, useRef, useState } from 'react';
import {
  Activity,
  Code2,
  Globe,
  LogOut,
  Menu,
  MessageSquare,
  Plus,
  RefreshCw,
  X,
} from 'lucide-react';
import { api, setCSRFToken } from './api';
import { AgentTimeline } from './components/AgentTimeline';
import { ApprovalBanner } from './components/ApprovalBanner';
import { CodeExplorer } from './components/CodeExplorer';
import { DiagnosticsView } from './components/DiagnosticsView';
import { HubFrameView } from './components/HubFrameView';
import { LoginView } from './components/LoginView';
import { PromptComposer } from './components/PromptComposer';
import { useAgentStream } from './hooks/useAgentStream';
import type { ConversationSummary } from './types';

type Tab = 'agent' | 'code' | 'hub' | 'status';

export function App() {
  const [authChecked, setAuthChecked] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [tlsEnabled, setTlsEnabled] = useState(true);
  const [tab, setTab] = useState<Tab>('agent');
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [activeConvId, setActiveConvId] = useState<string | null>(null);
  const initializedRef = useRef(false);
  const drawerRef = useRef<HTMLDialogElement>(null);

  const loadConversations = async (autoPickFirst = false) => {
    try {
      const res = await api.conversations();
      const list = res.conversations ?? [];
      setConversations(list);
      if (autoPickFirst && list.length > 0) {
        setActiveConvId((curr) => curr ?? list[0].id);
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
    if (authenticated && !initializedRef.current) {
      initializedRef.current = true;
      void loadConversations(true);
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
    return (
      <LoginView
        tlsEnabled={tlsEnabled}
        onSuccess={() => {
          initializedRef.current = false;
          setAuthenticated(true);
        }}
      />
    );
  }

  const activeConv = conversations.find((c) => c.id === activeConvId);
  const isLive = streamState === 'live' || streamState === 'polling';

  const openDrawer = () => {
    void loadConversations(false);
    drawerRef.current?.showModal();
  };

  const closeDrawer = () => {
    drawerRef.current?.close();
  };

  const handleLogout = async () => {
    try {
      await api.logout();
    } finally {
      initializedRef.current = false;
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
          <span className="session-label">
            {activeConv?.title || (activeConvId ? activeConvId : 'New Conversation')}
          </span>
          {activeConv ? (
            <span className="session-sub">
              {activeConv.step_count} steps · {activeConv.last_status}
            </span>
          ) : (
            <span className="session-sub">Send a prompt to start on laptop</span>
          )}
        </div>

        <div className="status-pill" aria-live="polite">
          <span
            className={`dot ${isLive ? 'live' : streamState === 'error' ? 'danger' : 'warn'}`}
            aria-hidden="true"
          />
          <span>
            {!activeConvId
              ? 'READY'
              : streamState === 'live'
                ? 'LIVE'
                : streamState === 'polling'
                  ? 'SYNC'
                  : streamState.toUpperCase()}
          </span>
        </div>
      </header>

      <main className="main-area">
        {tab === 'agent' && (
          <>
            <AgentTimeline
              steps={steps}
              streamState={streamState}
              conversationTitle={activeConv?.title || (activeConvId ? activeConvId : 'New Conversation')}
            />
            <ApprovalBanner
              conversationId={activeConvId || ''}
              onResolved={() => void loadConversations(false)}
              onOpenHub={() => setTab('hub')}
            />
            <PromptComposer
              conversationId={activeConvId || ''}
              onSent={(targetConvId) => {
                if (targetConvId && targetConvId !== activeConvId) {
                  setActiveConvId(targetConvId);
                }
                window.setTimeout(() => void loadConversations(false), 700);
              }}
              onNewChat={() => setActiveConvId(null)}
            />
          </>
        )}
        {tab === 'code' && <CodeExplorer />}
        {tab === 'hub' && <HubFrameView />}
        {tab === 'status' && <DiagnosticsView />}
      </main>

      <nav className="bottom-nav" aria-label="Primary navigation">
        <button
          type="button"
          className={tab === 'agent' ? 'active' : ''}
          onClick={() => setTab('agent')}
        >
          <MessageSquare size={19} aria-hidden="true" />
          <span>Chat</span>
        </button>
        <button
          type="button"
          className={tab === 'code' ? 'active' : ''}
          onClick={() => setTab('code')}
        >
          <Code2 size={19} aria-hidden="true" />
          <span>Code</span>
        </button>
        <button
          type="button"
          className={tab === 'hub' ? 'active' : ''}
          onClick={() => setTab('hub')}
        >
          <Globe size={19} aria-hidden="true" />
          <span>Hub</span>
        </button>
        <button
          type="button"
          className={tab === 'status' ? 'active' : ''}
          onClick={() => setTab('status')}
        >
          <Activity size={19} aria-hidden="true" />
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
              <button
                type="button"
                className="ghost"
                onClick={() => {
                  setActiveConvId(null);
                  setTab('agent');
                  closeDrawer();
                }}
                aria-label="Start new conversation"
                title="New Chat"
              >
                <Plus size={18} aria-hidden="true" />
              </button>
              <button
                type="button"
                className="ghost"
                onClick={() => void loadConversations(false)}
                aria-label="Refresh sessions"
              >
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

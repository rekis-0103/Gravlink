import { MouseEvent, useEffect, useMemo, useRef, useState } from 'react';
import DOMPurify from 'dompurify';
import { marked, type Tokens } from 'marked';
import {
  ArrowDown,
  Brain,
  ChevronRight,
  FileCode2,
  Globe,
  Search,
  Terminal,
  Wrench,
} from 'lucide-react';
import { escapeHtml, highlightSnippet } from '../lib/highlight';
import type { TranscriptStep } from '../types';
import type { StreamState } from '../hooks/useAgentStream';

interface AgentTimelineProps {
  steps: TranscriptStep[];
  streamState: StreamState;
  conversationTitle: string;
}

marked.use({
  breaks: true,
  gfm: true,
  renderer: {
    code({ text, lang }: Tokens.Code) {
      const safeLang = escapeHtml((lang || 'code').trim().split(/\s+/)[0] || 'code');
      const highlighted = highlightSnippet(text, lang);
      return `<div class="code-block-wrap"><div class="code-block-bar"><span class="code-lang-pill">${safeLang}</span><button type="button" class="code-copy-btn">Copy</button></div><pre><code class="hljs">${highlighted}</code></pre></div>`;
    },
  },
});

function formatStepText(step: TranscriptStep): string {
  const raw = step.content || '';
  if (step.type === 'USER_INPUT') {
    const match = raw.match(/<USER_REQUEST>([\s\S]*?)<\/USER_REQUEST>/);
    if (match) return match[1].trim();
  }
  return raw;
}

function renderSafeMarkdown(text: string): string {
  if (!text) return '';
  const html = marked.parse(text, { async: false }) as string;
  return DOMPurify.sanitize(html, {
    ADD_TAGS: ['button', 'span'],
    ADD_ATTR: ['type', 'class'],
  });
}

function formatArgs(args: unknown): string {
  if (args === undefined || args === null) return '';
  if (typeof args === 'string') return args;
  try {
    return JSON.stringify(args, null, 2);
  } catch {
    return String(args);
  }
}

function classifyTool(name: string) {
  const lower = name.toLowerCase();
  if (/(command|terminal|shell|exec|ps|bash)/.test(lower)) {
    return { cls: 'tool-cmd', label: 'CMD', Icon: Terminal };
  }
  if (/(write|edit|replace|create|patch|save|delete)/.test(lower)) {
    return { cls: 'tool-edit', label: 'EDIT', Icon: FileCode2 };
  }
  if (/(read|view|list|grep|find|search|outline)/.test(lower)) {
    return { cls: 'tool-read', label: 'READ', Icon: Search };
  }
  if (/(web|browser|fetch|url|http|mcp)/.test(lower)) {
    return { cls: 'tool-web', label: 'WEB', Icon: Globe };
  }
  return { cls: 'tool-other', label: 'TOOL', Icon: Wrench };
}

export function AgentTimeline({ steps, streamState, conversationTitle }: AgentTimelineProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pinnedToBottom, setPinnedToBottom] = useState(true);
  const [viewMode, setViewMode] = useState<'all' | 'chat'>('all');

  const visibleSteps = useMemo(() => {
    return steps.filter((s) => {
      const hasContent = Boolean(s.content?.trim());
      const hasThinking = Boolean(s.thinking?.trim());
      const hasTools = Boolean(s.tool_calls?.length);
      if (!hasContent && !hasThinking && !hasTools) return false;
      if (viewMode === 'chat') {
        return s.type === 'USER_INPUT' || (s.type === 'PLANNER_RESPONSE' && hasContent);
      }
      return (
        s.type === 'USER_INPUT' ||
        s.type === 'PLANNER_RESPONSE' ||
        hasContent ||
        hasThinking ||
        hasTools
      );
    });
  }, [steps, viewMode]);

  useEffect(() => {
    if (pinnedToBottom && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [visibleSteps.length, pinnedToBottom]);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    setPinnedToBottom(distance < 90);
  };

  const jumpToBottom = () => {
    if (!scrollRef.current) return;
    scrollRef.current.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
    setPinnedToBottom(true);
  };

  const handleTimelineClick = (ev: MouseEvent<HTMLDivElement>) => {
    const target = ev.target as HTMLElement | null;
    const btn = target?.closest('.code-copy-btn') as HTMLButtonElement | null;
    if (!btn) return;
    const codeText = btn.closest('.code-block-wrap')?.querySelector('pre')?.textContent ?? '';
    if (!codeText) return;
    void navigator.clipboard?.writeText(codeText).then(() => {
      btn.textContent = 'Copied ✓';
      window.setTimeout(() => {
        btn.textContent = 'Copy';
      }, 1500);
    });
  };

  return (
    <div className="chat-feed-wrap">
      <div className="timeline-toolbar">
        <span style={{ color: 'var(--muted)' }}>
          {visibleSteps.length} {visibleSteps.length === 1 ? 'step' : 'steps'}
        </span>
        <div className="segmented" role="group" aria-label="Timeline filter">
          <button
            type="button"
            className={viewMode === 'all' ? 'active' : ''}
            onClick={() => setViewMode('all')}
          >
            All Steps
          </button>
          <button
            type="button"
            className={viewMode === 'chat' ? 'active' : ''}
            onClick={() => setViewMode('chat')}
          >
            Chat Only
          </button>
        </div>
      </div>

      <div className="scroll-area" ref={scrollRef} onScroll={handleScroll}>
        {visibleSteps.length === 0 ? (
          <div className="chat-empty-state">
            <strong>{conversationTitle}</strong>
            <span>
              {streamState === 'connecting'
                ? 'Connecting to live transcript stream...'
                : 'Send a prompt below or pick an active session from the menu.'}
            </span>
          </div>
        ) : (
          <div className="timeline-list" onClick={handleTimelineClick}>
            {visibleSteps.map((step) => {
              const isUser = step.type === 'USER_INPUT';
              const isPlanner = step.type === 'PLANNER_RESPONSE';
              const isRunning = step.status !== 'DONE';
              const bodyText = formatStepText(step);
              const safeHtml = renderSafeMarkdown(bodyText);
              const timeLabel = step.created_at
                ? new Date(step.created_at).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                  })
                : '';

              if (isUser) {
                return (
                  <article key={step.step_index} className="msg-user-row">
                    <div className="msg-user-bubble">
                      {bodyText && (
                        <div className="markdown-body" dangerouslySetInnerHTML={{ __html: safeHtml }} />
                      )}
                    </div>
                    {timeLabel && <span className="msg-timestamp">You · {timeLabel}</span>}
                  </article>
                );
              }

              return (
                <article
                  key={step.step_index}
                  className={`msg-assistant ${!isPlanner ? 'step-system' : ''} ${isRunning ? 'running' : ''}`}
                >
                  <div className="msg-assistant-header">
                    <span className="step-badge">
                      {isRunning && <span className="dot live" aria-hidden="true" />}
                      {isPlanner ? 'Antigravity' : step.type}
                    </span>
                    <span>
                      {isRunning ? `${step.status || 'RUNNING'} · ` : ''}
                      {timeLabel}
                    </span>
                  </div>

                  {step.thinking && (
                    <details className="thinking-box" open={isRunning && !bodyText}>
                      <summary>
                        <ChevronRight size={14} aria-hidden="true" />
                        <Brain size={14} aria-hidden="true" />
                        <span>Thinking process</span>
                      </summary>
                      <div
                        className="markdown-body"
                        style={{ marginTop: '0.45rem', color: 'var(--muted)' }}
                        dangerouslySetInnerHTML={{ __html: renderSafeMarkdown(step.thinking) }}
                      />
                    </details>
                  )}

                  {bodyText && (
                    <div className="markdown-body" dangerouslySetInnerHTML={{ __html: safeHtml }} />
                  )}

                  {step.tool_calls && step.tool_calls.length > 0 && (
                    <div className="tool-chips" aria-label="Tool calls">
                      {step.tool_calls.map((tc, idx) => {
                        const argsText = formatArgs(tc.args);
                        const { cls, label, Icon } = classifyTool(tc.name);
                        const highlightedArgs = argsText ? highlightSnippet(argsText, 'json') : '';
                        return (
                          <details key={`${step.step_index}-tc-${idx}`} className={`tool-details ${cls}`}>
                            <summary>
                              <Icon size={13} aria-hidden="true" style={{ color: 'var(--tool-color)', flexShrink: 0 }} />
                              <span className="tool-tag">{label}</span>
                              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{tc.name}</span>
                            </summary>
                            {argsText && (
                              <pre className="tool-args-pre">
                                <code
                                  className="hljs"
                                  dangerouslySetInnerHTML={{
                                    __html: DOMPurify.sanitize(highlightedArgs, {
                                      ADD_TAGS: ['span'],
                                      ADD_ATTR: ['class'],
                                    }),
                                  }}
                                />
                              </pre>
                            )}
                          </details>
                        );
                      })}
                    </div>
                  )}
                </article>
              );
            })}
          </div>
        )}
      </div>

      {!pinnedToBottom && (
        <button
          type="button"
          className="primary jump-latest-btn"
          onClick={jumpToBottom}
          aria-label="Jump to latest message"
        >
          <ArrowDown size={16} aria-hidden="true" />
          Latest
        </button>
      )}
    </div>
  );
}

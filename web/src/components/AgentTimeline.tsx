import { useEffect, useMemo, useRef, useState } from 'react';
import DOMPurify from 'dompurify';
import { marked } from 'marked';
import { ArrowDown, Brain, ChevronRight, Wrench } from 'lucide-react';
import type { TranscriptStep } from '../types';
import type { StreamState } from '../hooks/useAgentStream';

interface AgentTimelineProps {
  steps: TranscriptStep[];
  streamState: StreamState;
  conversationTitle: string;
}

marked.setOptions({
  breaks: true,
  gfm: true,
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
  return DOMPurify.sanitize(html);
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

export function AgentTimeline({ steps, streamState, conversationTitle }: AgentTimelineProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pinnedToBottom, setPinnedToBottom] = useState(true);
  const [viewMode, setViewMode] = useState<'all' | 'chat'>('all');

  const visibleSteps = useMemo(() => {
    return steps.filter((s) => {
      const hasContent = Boolean(s.content?.trim());
      const hasTools = Boolean(s.tool_calls?.length);
      if (!hasContent && !hasTools) return false;
      if (viewMode === 'chat') {
        return s.type === 'USER_INPUT' || (s.type === 'PLANNER_RESPONSE' && hasContent);
      }
      return s.type === 'USER_INPUT' || s.type === 'PLANNER_RESPONSE' || hasContent || hasTools;
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

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minBlockSize: 0, position: 'relative' }}>
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
          <div className="diag-card" style={{ textAlign: 'center', color: 'var(--muted)' }}>
            <p>
              {streamState === 'connecting'
                ? 'Connecting to live transcript stream...'
                : `No visible steps yet for "${conversationTitle}".`}
            </p>
          </div>
        ) : (
          <div className="timeline-list">
            {visibleSteps.map((step) => {
              const isUser = step.type === 'USER_INPUT';
              const isRunning = step.status !== 'DONE';
              const bodyText = formatStepText(step);
              const safeHtml = renderSafeMarkdown(bodyText);
              const timeLabel = step.created_at
                ? new Date(step.created_at).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                  })
                : '';

              return (
                <article
                  key={step.step_index}
                  className={`step-card ${isUser ? 'user' : ''} ${isRunning ? 'running' : ''}`}
                >
                  <div className="step-meta">
                    <span className="step-badge">
                      {isRunning && <span className="dot warn" aria-hidden="true" />}
                      {isUser ? 'You' : step.type === 'PLANNER_RESPONSE' ? 'Antigravity' : step.type}
                    </span>
                    <span>
                      {isRunning ? `${step.status} · ` : ''}
                      {timeLabel}
                    </span>
                  </div>

                  {step.thinking && (
                    <details className="thinking-box">
                      <summary>
                        <ChevronRight size={14} aria-hidden="true" style={{ marginRight: '0.25rem' }} />
                        <Brain size={14} aria-hidden="true" style={{ marginRight: '0.35rem' }} />
                        Thinking process
                      </summary>
                      <div
                        className="markdown-body"
                        style={{ marginTop: '0.5rem', color: 'var(--muted)' }}
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
                        return (
                          <details key={`${step.step_index}-tc-${idx}`} className="tool-details">
                            <summary>
                              <Wrench size={12} aria-hidden="true" style={{ color: 'var(--accent)' }} />
                              <span>{tc.name}</span>
                            </summary>
                            {argsText && <pre className="tool-args-pre">{argsText}</pre>}
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

      <div className="mvp-notice">
        <span>Read-Only Live View · {streamState === 'polling' ? 'HTTPS Poll' : streamState.toUpperCase()}</span>
        <span>Prompt &amp; Approval: M2</span>
      </div>
    </div>
  );
}

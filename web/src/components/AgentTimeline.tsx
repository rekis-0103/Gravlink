import { memo, MouseEvent, useEffect, useMemo, useRef, useState } from 'react';
import DOMPurify from 'dompurify';
import { marked, type Tokens } from 'marked';
import {
  ArrowDown,
  Brain,
  ChevronRight,
  Clock,
  FileCode2,
  Globe,
  History,
  Search,
  Terminal,
  Wrench,
  Zap,
} from 'lucide-react';
import { detectAgentRunning, isActiveStepStatus } from '../lib/agentStatus';
import { formatDurationMs, formatTokens } from '../lib/format';
import { escapeHtml, highlightSnippet } from '../lib/highlight';
import type { TranscriptStep } from '../types';
import type { StreamState } from '../hooks/useAgentStream';

const INITIAL_RENDER_LIMIT = 45;
const RENDER_PAGE_STEP = 45;

interface AgentTimelineProps {
  steps: TranscriptStep[];
  streamState: StreamState;
  conversationTitle: string;
  conversationStatus?: string;
  onOpenTokens?: () => void;
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

function parseStepMs(iso?: string): number | null {
  if (!iso) return null;
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? null : ms;
}

/** Isolated 1-second timer component so ticking never re-renders the chat feed. */
const LiveElapsedClock = memo(function LiveElapsedClock({ startMs }: { startMs: number | null }) {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    setNowMs(Date.now());
    const id = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [startMs]);

  const elapsed = startMs !== null ? Math.max(0, nowMs - startMs) : 0;
  return <>{formatDurationMs(elapsed)}</>;
});

interface StepItemProps {
  step: TranscriptStep;
  stepElapsed?: number;
  isLiveTail: boolean;
}

const StepItem = memo(function StepItem({ step, stepElapsed, isLiveTail }: StepItemProps) {
  const isUser = step.type === 'USER_INPUT';
  const isPlanner = step.type === 'PLANNER_RESPONSE';
  const isStepRunning = isLiveTail && isActiveStepStatus(step.status);

  const bodyText = useMemo(() => formatStepText(step), [step.type, step.content]);
  const safeHtml = useMemo(() => renderSafeMarkdown(bodyText), [bodyText]);
  const safeThinkingHtml = useMemo(
    () => (step.thinking ? renderSafeMarkdown(step.thinking) : ''),
    [step.thinking]
  );
  const highlightedTools = useMemo(() => {
    if (!step.tool_calls?.length) return [];
    return step.tool_calls.map((tc) => {
      const argsText = formatArgs(tc.args);
      const html = argsText
        ? DOMPurify.sanitize(highlightSnippet(argsText, 'json'), {
            ADD_TAGS: ['span'],
            ADD_ATTR: ['class'],
          })
        : '';
      return { name: tc.name, argsHtml: html, ...classifyTool(tc.name) };
    });
  }, [step.tool_calls]);

  const timeLabel = useMemo(() => {
    if (!step.created_at) return '';
    return new Date(step.created_at).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
    });
  }, [step.created_at]);

  if (isUser) {
    return (
      <article className="msg-user-row">
        <div className="msg-user-bubble">
          {bodyText && <div className="markdown-body" dangerouslySetInnerHTML={{ __html: safeHtml }} />}
        </div>
        {timeLabel && <span className="msg-timestamp">You · {timeLabel}</span>}
      </article>
    );
  }

  const stepIn = step.input_tokens || 0;
  const stepCache = step.cache_read_tokens || 0;
  const stepOut = step.output_tokens || 0;
  const stepTotalTok = stepIn + stepCache + stepOut;
  const createdMs = isStepRunning ? parseStepMs(step.created_at) : null;

  return (
    <article
      className={`msg-assistant ${!isPlanner ? 'step-system' : ''} ${isStepRunning ? 'running' : ''}`}
    >
      <div className="msg-assistant-header">
        <span className="step-badge">
          {isStepRunning && <span className="dot live" aria-hidden="true" />}
          {isPlanner ? 'Antigravity' : step.type}
        </span>
        <span className="step-header-right">
          {isStepRunning ? (
            <>
              RUNNING · ⏱ <LiveElapsedClock startMs={createdMs} />
            </>
          ) : stepElapsed !== undefined ? (
            `⏱ ${formatDurationMs(stepElapsed)} · ${timeLabel}`
          ) : (
            timeLabel
          )}
        </span>
      </div>

      {step.thinking && (
        <details className="thinking-box" open={isStepRunning && !bodyText}>
          <summary>
            <ChevronRight size={14} aria-hidden="true" />
            <Brain size={14} aria-hidden="true" />
            <span>Thinking process</span>
          </summary>
          <div
            className="markdown-body"
            style={{ marginTop: '0.45rem', color: 'var(--muted)' }}
            dangerouslySetInnerHTML={{ __html: safeThinkingHtml }}
          />
        </details>
      )}

      {bodyText && <div className="markdown-body" dangerouslySetInnerHTML={{ __html: safeHtml }} />}

      {highlightedTools.length > 0 && (
        <div className="tool-chips" aria-label="Tool calls">
          {highlightedTools.map((tc, idx) => {
            const Icon = tc.Icon;
            return (
              <details key={`${step.step_index}-tc-${idx}`} className={`tool-details ${tc.cls}`}>
                <summary>
                  <Icon size={13} aria-hidden="true" style={{ color: 'var(--tool-color)', flexShrink: 0 }} />
                  <span className="tool-tag">{tc.label}</span>
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{tc.name}</span>
                </summary>
                {tc.argsHtml && (
                  <pre className="tool-args-pre">
                    <code className="hljs" dangerouslySetInnerHTML={{ __html: tc.argsHtml }} />
                  </pre>
                )}
              </details>
            );
          })}
        </div>
      )}

      {stepTotalTok > 0 && (
        <div className="step-metrics">
          <span>⚡ {formatTokens(stepTotalTok)} tok</span>
          <span>
            ↑{formatTokens(stepIn)} · ♻{formatTokens(stepCache)} · ↓{formatTokens(stepOut)}
          </span>
        </div>
      )}
    </article>
  );
});

export function AgentTimeline({
  steps,
  streamState,
  conversationTitle,
  conversationStatus,
  onOpenTokens,
}: AgentTimelineProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pinnedToBottom, setPinnedToBottom] = useState(true);
  const [viewMode, setViewMode] = useState<'all' | 'chat'>('all');
  const [renderLimit, setRenderLimit] = useState(INITIAL_RENDER_LIMIT);

  // Reset window limit whenever the user switches to a different conversation
  useEffect(() => {
    setRenderLimit(INITIAL_RENDER_LIMIT);
    setPinnedToBottom(true);
  }, [conversationTitle]);

  const stats = useMemo(() => {
    let sessionIn = 0;
    let sessionCache = 0;
    let sessionOut = 0;
    let sessionDurationMs = 0;
    let turnTotal = 0;
    let turnStartMs: number | null = null;
    let lastUserIdx = -1;

    steps.forEach((s, idx) => {
      if (s.type === 'USER_INPUT') lastUserIdx = idx;
      sessionIn += s.input_tokens || 0;
      sessionCache += s.cache_read_tokens || 0;
      sessionOut += s.output_tokens || 0;
    });

    let curTurnStart: number | null = null;
    let curTurnEnd: number | null = null;
    for (const s of steps) {
      const ts = parseStepMs(s.created_at);
      if (s.type === 'USER_INPUT') {
        if (curTurnStart !== null && curTurnEnd !== null && curTurnEnd > curTurnStart) {
          sessionDurationMs += curTurnEnd - curTurnStart;
        }
        curTurnStart = ts;
        curTurnEnd = ts;
      } else if (curTurnStart !== null && ts !== null && ts > (curTurnEnd ?? 0)) {
        curTurnEnd = ts;
      }
    }
    if (curTurnStart !== null && curTurnEnd !== null && curTurnEnd > curTurnStart) {
      sessionDurationMs += curTurnEnd - curTurnStart;
    }

    const activeSlice = lastUserIdx >= 0 ? steps.slice(lastUserIdx) : steps;
    for (const s of activeSlice) {
      turnTotal += (s.input_tokens || 0) + (s.cache_read_tokens || 0) + (s.output_tokens || 0);
      const ts = parseStepMs(s.created_at);
      if (ts !== null && (turnStartMs === null || ts < turnStartMs)) {
        turnStartMs = ts;
      }
    }

    const { isRunning, activeStep } = detectAgentRunning(steps, conversationStatus);
    let activityLabel = 'AI sedang berjalan...';
    if (activeStep) {
      if (activeStep.tool_calls?.length) {
        activityLabel = `Menjalankan ${activeStep.tool_calls[0].name}...`;
      } else if (activeStep.thinking && !activeStep.content) {
        activityLabel = 'AI sedang bernalar...';
      } else {
        activityLabel = 'Menulis respons...';
      }
    } else if (isRunning) {
      activityLabel = 'Memproses permintaan...';
    }

    return {
      sessionTotal: sessionIn + sessionCache + sessionOut,
      sessionDurationMs,
      turnTotal,
      turnStartMs,
      isRunning,
      activityLabel,
    };
  }, [steps, conversationStatus]);

  const stepDurationByIndex = useMemo(() => {
    const map = new Map<number, number>();
    let prevMs: number | null = null;
    for (const s of steps) {
      const ts = parseStepMs(s.created_at);
      if (s.type !== 'USER_INPUT' && prevMs !== null && ts !== null && ts > prevMs) {
        const diff = ts - prevMs;
        if (diff < 3600_000) {
          map.set(s.step_index, diff);
        }
      }
      if (ts !== null) prevMs = ts;
    }
    return map;
  }, [steps]);

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

  const hiddenCount = Math.max(0, visibleSteps.length - renderLimit);
  const displayedSteps = useMemo(
    () => (hiddenCount > 0 ? visibleSteps.slice(hiddenCount) : visibleSteps),
    [visibleSteps, hiddenCount]
  );
  const lastStepIndex = steps.length > 0 ? steps[steps.length - 1].step_index : -1;

  useEffect(() => {
    if (pinnedToBottom && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [displayedSteps.length, lastStepIndex, pinnedToBottom]);

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
        <button
          type="button"
          className="toolbar-token-btn"
          onClick={onOpenTokens}
          title="Lihat detail penggunaan token"
        >
          <Zap size={13} aria-hidden="true" />
          <span>{formatTokens(stats.sessionTotal)} tok</span>
          {stats.sessionDurationMs > 0 && (
            <span className="toolbar-sep">· ⏱ {formatDurationMs(stats.sessionDurationMs)}</span>
          )}
        </button>

        <div className="segmented" role="group" aria-label="Timeline filter">
          <button
            type="button"
            className={viewMode === 'all' ? 'active' : ''}
            onClick={() => setViewMode('all')}
          >
            All
          </button>
          <button
            type="button"
            className={viewMode === 'chat' ? 'active' : ''}
            onClick={() => setViewMode('chat')}
          >
            Chat
          </button>
        </div>
      </div>

      {stats.isRunning && (
        <div
          className="ai-live-banner"
          role="status"
          aria-live="polite"
          onClick={onOpenTokens}
        >
          <div className="ai-live-left">
            <span className="eq-bars" aria-hidden="true">
              <i />
              <i />
              <i />
            </span>
            <span className="ai-live-label">{stats.activityLabel}</span>
          </div>
          <div className="ai-live-metrics">
            <span className="ai-live-pill">
              <Clock size={12} aria-hidden="true" />
              <LiveElapsedClock startMs={stats.turnStartMs} />
            </span>
            <span className="ai-live-pill">
              <Zap size={12} aria-hidden="true" />
              {formatTokens(stats.turnTotal > 0 ? stats.turnTotal : stats.sessionTotal)}
            </span>
          </div>
        </div>
      )}

      <div className="scroll-area" ref={scrollRef} onScroll={handleScroll}>
        {displayedSteps.length === 0 ? (
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
            {hiddenCount > 0 && (
              <button
                type="button"
                className="load-older-btn"
                onClick={() => setRenderLimit((n) => n + RENDER_PAGE_STEP)}
              >
                <History size={14} aria-hidden="true" />
                <span>
                  Muat {Math.min(RENDER_PAGE_STEP, hiddenCount)} pesan sebelumnya (sisa {hiddenCount})
                </span>
              </button>
            )}

            {displayedSteps.map((step) => (
              <StepItem
                key={step.step_index}
                step={step}
                stepElapsed={stepDurationByIndex.get(step.step_index)}
                isLiveTail={step.step_index === lastStepIndex}
              />
            ))}
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

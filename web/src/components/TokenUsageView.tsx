import { useEffect, useState } from 'react';
import { ArrowLeft, Clock, Cpu, RefreshCw, Sparkles, Zap } from 'lucide-react';
import { api } from '../api';
import { formatDurationMs, formatTokens } from '../lib/format';
import type { TokenUsageReport } from '../types';

interface TokenUsageViewProps {
  activeConvId: string | null;
  onSelectSession: (id: string) => void;
  onBackToChat: () => void;
}

export function TokenUsageView({
  activeConvId,
  onSelectSession,
  onBackToChat,
}: TokenUsageViewProps) {
  const [report, setReport] = useState<TokenUsageReport | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api.tokenUsage();
      setReport(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load token usage');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const total = report?.total_tokens || 0;
  const inPct = total > 0 ? Math.round(((report?.total_input_tokens || 0) / total) * 100) : 0;
  const cachePct =
    total > 0 ? Math.round(((report?.total_cache_read_tokens || 0) / total) * 100) : 0;
  const outPct = total > 0 ? Math.max(0, 100 - inPct - cachePct) : 0;

  return (
    <div className="scroll-area">
      <div className="token-header-row">
        <button type="button" className="compact" onClick={onBackToChat}>
          <ArrowLeft size={15} aria-hidden="true" />
          <span>Chat</span>
        </button>
        <strong style={{ fontSize: '0.95rem', flex: 1 }}>Penggunaan Token</strong>
        <button type="button" className="compact" onClick={() => void load()} disabled={loading}>
          <RefreshCw size={15} aria-hidden="true" />
          <span>{loading ? '...' : 'Refresh'}</span>
        </button>
      </div>

      {error && (
        <div className="error-box" style={{ marginBottom: '0.75rem' }}>
          {error}
        </div>
      )}

      {report && (
        <>
          <div className="token-stats-grid">
            <div className="token-stat-card stat-total">
              <div className="token-stat-label">
                <Zap size={14} aria-hidden="true" />
                <span>Total Token</span>
              </div>
              <div className="token-stat-val">{formatTokens(report.total_tokens)}</div>
              <div className="token-stat-sub">
                {report.sessions.length} sesi · {report.total_turns} prompt
              </div>
            </div>

            <div className="token-stat-card stat-time">
              <div className="token-stat-label">
                <Clock size={14} aria-hidden="true" />
                <span>Waktu Aktif AI</span>
              </div>
              <div className="token-stat-val">{formatDurationMs(report.total_duration_ms)}</div>
              <div className="token-stat-sub">Akumulasi durasi kerja</div>
            </div>

            <div className="token-stat-card stat-input">
              <div className="token-stat-label">
                <Cpu size={14} aria-hidden="true" />
                <span>Input + Cache</span>
              </div>
              <div className="token-stat-val">
                {formatTokens(report.total_input_tokens + report.total_cache_read_tokens)}
              </div>
              <div className="token-stat-sub">
                Baru {formatTokens(report.total_input_tokens)} · Cache{' '}
                {formatTokens(report.total_cache_read_tokens)}
              </div>
            </div>

            <div className="token-stat-card stat-output">
              <div className="token-stat-label">
                <Sparkles size={14} aria-hidden="true" />
                <span>Output AI</span>
              </div>
              <div className="token-stat-val">{formatTokens(report.total_output_tokens)}</div>
              <div className="token-stat-sub">Cache hit {cachePct}%</div>
            </div>
          </div>

          <section className="diag-card" aria-label="Distribusi token">
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.45rem', fontSize: '0.82rem' }}>
              <strong>Proporsi Token</strong>
              <span style={{ color: 'var(--muted)' }}>{report.total_tokens.toLocaleString()} tok</span>
            </div>
            <div className="token-bar-track" aria-hidden="true">
              <div className="token-bar-seg seg-input" style={{ width: `${inPct}%` }} />
              <div className="token-bar-seg seg-cache" style={{ width: `${cachePct}%` }} />
              <div className="token-bar-seg seg-output" style={{ width: `${outPct}%` }} />
            </div>
            <div className="token-legend">
              <span>
                <i className="legend-dot seg-input" /> Input ({inPct}%)
              </span>
              <span>
                <i className="legend-dot seg-cache" /> Cache ({cachePct}%)
              </span>
              <span>
                <i className="legend-dot seg-output" /> Output ({outPct}%)
              </span>
            </div>
          </section>

          <h2 style={{ fontSize: '0.9rem', margin: '0.85rem 0 0.5rem' }}>
            Pemakaian per Sesi ({report.sessions.length})
          </h2>

          <div className="token-session-list">
            {report.sessions.map((s) => {
              const isCurrent = s.id === activeConvId;
              const isRunning =
                s.last_status !== 'DONE' &&
                !s.last_status.includes('IDLE') &&
                s.last_status !== '';
              const pctOfMax =
                total > 0 ? Math.max(4, Math.round((s.total_tokens / total) * 100)) : 0;

              return (
                <button
                  key={s.id}
                  type="button"
                  className={`token-session-card ${isCurrent ? 'active' : ''}`}
                  onClick={() => onSelectSession(s.id)}
                >
                  <div className="token-session-top">
                    <span className="token-session-title">
                      {isRunning && <span className="dot live" aria-hidden="true" />}
                      {s.title}
                    </span>
                    <span className="token-pill-total">{formatTokens(s.total_tokens)} tok</span>
                  </div>

                  <div className="token-session-bar" aria-hidden="true">
                    <div className="token-session-fill" style={{ width: `${pctOfMax}%` }} />
                  </div>

                  <div className="token-session-meta">
                    <span>
                      ↑ {formatTokens(s.input_tokens)} · ♻ {formatTokens(s.cache_read_tokens)} · ↓{' '}
                      {formatTokens(s.output_tokens)}
                    </span>
                    <span>
                      ⏱ {formatDurationMs(s.duration_ms)} · {s.turns} prompt
                    </span>
                  </div>
                </button>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

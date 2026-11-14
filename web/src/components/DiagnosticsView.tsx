import { useEffect, useState } from 'react';
import { Download, RefreshCw, ShieldAlert, ShieldCheck } from 'lucide-react';
import { api } from '../api';
import type { DoctorCheck, SystemStatus } from '../types';

export function DiagnosticsView() {
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [checks, setChecks] = useState<DoctorCheck[]>([]);
  const [loading, setLoading] = useState(false);

  const refresh = async () => {
    setLoading(true);
    try {
      const [st, doc] = await Promise.all([api.status(), api.doctor()]);
      setStatus(st);
      setChecks(doc.checks ?? []);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  return (
    <div className="scroll-area">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem', gap: '0.5rem' }}>
        <h2 style={{ fontSize: '1rem' }}>Host &amp; Antigravity Diagnostics</h2>
        <button type="button" className="compact" onClick={() => void refresh()} disabled={loading}>
          <RefreshCw size={15} aria-hidden="true" />
          Refresh
        </button>
      </div>

      {status && (
        <section className="diag-card">
          <div className="diag-row">
            <strong>Workspace</strong>
            <span className="diag-value">{status.workspace}</span>
          </div>
          <div className="diag-row">
            <strong>Live Antigravity Hubs</strong>
            <span className="diag-value">
              {status.hubs?.length
                ? `${status.hubs.length} active (port ${status.hubs[0].httpPort}, LS v${status.hubs[0].lsVersion})`
                : 'None detected'}
            </span>
          </div>
          <div className="diag-row">
            <strong>Windows Firewall (Private)</strong>
            <span className="diag-value" style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem' }}>
              {status.firewall_rule_ok ? (
                <ShieldCheck size={15} style={{ color: 'var(--ok)', flexShrink: 0 }} aria-hidden="true" />
              ) : (
                <ShieldAlert size={15} style={{ color: 'var(--warn)', flexShrink: 0 }} aria-hidden="true" />
              )}
              <span>{status.firewall_detail}</span>
            </span>
          </div>
          {status.tls_enabled && (
            <div className="diag-row">
              <strong>Local Root CA (for WSS &amp; PWA)</strong>
              <a
                href="/ca.crt"
                style={{
                  color: 'var(--accent)',
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '0.35rem',
                  fontWeight: 600,
                }}
              >
                <Download size={15} aria-hidden="true" />
                Download gravlink-local-ca.crt
              </a>
            </div>
          )}
        </section>
      )}

      <section className="diag-card">
        <h3 style={{ fontSize: '0.92rem', marginBottom: '0.5rem' }}>Antigravity Contract Checks (`gravlink doctor`)</h3>
        {checks.map((c) => (
          <div key={c.name} className="diag-row">
            <div>
              <div style={{ fontWeight: 600 }}>{c.name}</div>
              <div className="diag-value" style={{ textAlign: 'left' }}>{c.detail}</div>
            </div>
            <span
              style={{
                fontWeight: 700,
                fontSize: '0.78rem',
                flexShrink: 0,
                color: c.status === 'PASS' ? 'var(--ok)' : c.status === 'WARN' ? 'var(--warn)' : 'var(--danger)',
              }}
            >
              {c.status}
            </span>
          </div>
        ))}
      </section>
    </div>
  );
}

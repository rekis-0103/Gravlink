import { useState } from 'react';

export function HubFrameView() {
  const [reloadKey, setReloadKey] = useState(0);

  return (
    <section className="hub-shell" aria-label="Antigravity Live Hub">
      <div className="hub-toolbar">
        <span style={{ color: 'var(--muted)' }}>
          Antigravity Jetski UI (LAN reverse proxy + mobile patch)
        </span>
        <div style={{ display: 'flex', gap: '0.4rem' }}>
          <button
            type="button"
            className="compact"
            onClick={() => setReloadKey((k) => k + 1)}
          >
            Reload Hub
          </button>
          <a
            href="/hub/"
            target="_blank"
            rel="noopener noreferrer"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              fontSize: '0.78rem',
              paddingInline: '0.45rem',
              color: 'var(--accent)',
              textDecoration: 'none',
            }}
          >
            Fullscreen ↗
          </a>
        </div>
      </div>

      <iframe
        key={reloadKey}
        src="/hub/"
        title="Antigravity Live Hub"
        className="hub-iframe"
        allow="clipboard-read; clipboard-write"
      />
    </section>
  );
}

import { FormEvent, useEffect, useState } from 'react';
import { ExternalLink, RefreshCw, Search } from 'lucide-react';
import { api } from '../api';
import type { DevServerPort } from '../types';

export function PreviewView() {
  const [ports, setPorts] = useState<DevServerPort[]>([]);
  const [scanning, setScanning] = useState(false);
  const [selectedPort, setSelectedPort] = useState<number | null>(null);
  const [portInput, setPortInput] = useState('5173');
  const [reloadKey, setReloadKey] = useState(0);

  const scanPorts = async (autoSelect = false) => {
    setScanning(true);
    try {
      const res = await api.previewPorts();
      const list = res.ports ?? [];
      setPorts(list);
      const firstActive = list.find((p) => p.active);
      if (autoSelect && firstActive && selectedPort === null) {
        setSelectedPort(firstActive.port);
        setPortInput(String(firstActive.port));
      }
    } catch {
      // Handled gracefully
    } finally {
      setScanning(false);
    }
  };

  useEffect(() => {
    void scanPorts(true);
  }, []);

  const openPort = (port: number) => {
    if (port < 1024 || port > 65535) return;
    setSelectedPort(port);
    setPortInput(String(port));
    setReloadKey((k) => k + 1);
  };

  const handleManualSubmit = (e: FormEvent) => {
    e.preventDefault();
    const parsed = parseInt(portInput.trim(), 10);
    if (!Number.isNaN(parsed) && parsed >= 1024 && parsed <= 65535) {
      openPort(parsed);
    }
  };

  const activePorts = ports.filter((p) => p.active);
  const quickPresets = [5173, 3000, 8080, 8000];

  return (
    <section className="hub-shell" aria-label="Local Dev Server Preview">
      <div className="preview-toolbar">
        <form className="preview-port-form" onSubmit={handleManualSubmit}>
          <label htmlFor="preview-port-input" className="visually-hidden">
            Localhost port
          </label>
          <input
            id="preview-port-input"
            type="text"
            inputMode="numeric"
            pattern="[0-9]*"
            className="preview-port-input"
            value={portInput}
            onChange={(e) => setPortInput(e.target.value)}
            placeholder="Port (e.g. 5173)"
          />
          <button type="submit" className="compact primary">
            Open
          </button>
        </form>

        <div style={{ display: 'flex', gap: '0.35rem', alignItems: 'center' }}>
          <button
            type="button"
            className="compact"
            onClick={() => void scanPorts(false)}
            disabled={scanning}
            aria-label="Scan local ports"
          >
            <Search size={14} aria-hidden="true" />
            <span>{scanning ? '...' : 'Scan'}</span>
          </button>

          {selectedPort !== null && (
            <>
              <button
                type="button"
                className="compact"
                onClick={() => setReloadKey((k) => k + 1)}
                aria-label="Reload preview"
              >
                <RefreshCw size={14} aria-hidden="true" />
              </button>
              <a
                href={`/preview/${selectedPort}/`}
                target="_blank"
                rel="noopener noreferrer"
                className="preview-ext-link"
                aria-label="Open preview fullscreen"
              >
                <ExternalLink size={14} aria-hidden="true" />
              </a>
            </>
          )}
        </div>
      </div>

      <div className="preview-chips-row">
        {activePorts.map((p) => (
          <button
            key={p.port}
            type="button"
            className={`preview-chip ${selectedPort === p.port ? 'active' : ''}`}
            onClick={() => openPort(p.port)}
          >
            <span className="dot live" aria-hidden="true" />
            <span>
              :{p.port} · {p.label}
            </span>
          </button>
        ))}

        {activePorts.length === 0 &&
          quickPresets.map((p) => (
            <button
              key={p}
              type="button"
              className={`preview-chip ${selectedPort === p ? 'active' : ''}`}
              onClick={() => openPort(p)}
            >
              <span>:{p}</span>
            </button>
          ))}
      </div>

      {selectedPort !== null ? (
        <iframe
          key={`${selectedPort}-${reloadKey}`}
          src={`/preview/${selectedPort}/`}
          title={`Localhost Preview Port ${selectedPort}`}
          className="hub-iframe"
        />
      ) : (
        <div className="scroll-area">
          <div className="chat-empty-state">
            <strong>Dev Server Preview</strong>
            <span>
              {activePorts.length > 0
                ? 'Tap a live port pill above to preview your local web app on your phone.'
                : 'No local dev server detected on common ports yet. Run npm run dev or your server on the laptop, then tap Scan or choose a port above.'}
            </span>
          </div>
        </div>
      )}
    </section>
  );
}

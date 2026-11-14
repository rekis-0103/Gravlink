import { useState, type FormEvent } from 'react';
import { Eye, EyeOff, Lock, ShieldCheck } from 'lucide-react';
import { api, setCSRFToken } from '../api';

interface LoginViewProps {
  tlsEnabled: boolean;
  onSuccess: () => void;
}

export function LoginView({ tlsEnabled, onSuccess }: LoginViewProps) {
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!password) return;
    setSubmitting(true);
    setError('');
    try {
      const res = await api.login(password);
      if (res.csrf_token) setCSRFToken(res.csrf_token);
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="login-page">
      <section className="login-card" aria-labelledby="login-heading">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
          <ShieldCheck size={28} aria-hidden="true" style={{ color: 'var(--accent)' }} />
          <div>
            <h1 id="login-heading" style={{ fontSize: '1.25rem' }}>
              Gravlink
            </h1>
            <p style={{ margin: 0, fontSize: '0.82rem', color: 'var(--muted)' }}>
              LAN-only Antigravity IDE Companion
            </p>
          </div>
        </div>

        {error && (
          <div className="error-box" role="alert">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '0.9rem' }}>
          <input
            type="text"
            name="username"
            autoComplete="username"
            defaultValue="gravlink-host"
            readOnly
            tabIndex={-1}
            aria-hidden="true"
            style={{ position: 'absolute', opacity: 0, pointerEvents: 'none', width: 1, height: 1 }}
          />
          <div className="field-group">
            <label htmlFor="current-password">Host Password</label>
            <div className="password-row">
              <input
                id="current-password"
                name="password"
                type={showPassword ? 'text' : 'password'}
                autoComplete="current-password"
                enterKeyHint="done"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter Gravlink password"
              />
              <button
                type="button"
                onClick={() => setShowPassword((prev) => !prev)}
                aria-label={showPassword ? 'Hide password' : 'Show password'}
                aria-pressed={showPassword}
              >
                {showPassword ? <EyeOff size={18} aria-hidden="true" /> : <Eye size={18} aria-hidden="true" />}
              </button>
            </div>
          </div>

          <button type="submit" className="primary" disabled={submitting}>
            <Lock size={17} aria-hidden="true" />
            {submitting ? 'Signing in...' : 'Sign In'}
          </button>
        </form>

        <p style={{ margin: 0, fontSize: '0.78rem', color: 'var(--muted)' }}>
          Tip: You can also scan the one-time QR code shown in the laptop terminal to sign in automatically.
          {tlsEnabled && (
            <>
              {' '}
              <a href="/ca.crt" style={{ color: 'var(--accent)' }}>
                Download Local Root CA
              </a>
            </>
          )}
        </p>
      </section>
    </main>
  );
}

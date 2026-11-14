/** Formats a token integer into compact human-readable notation (e.g. 1.2k, 2.4M). */
export function formatTokens(count?: number): string {
  const n = Number(count || 0);
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 10_000) return `${(n / 1_000).toFixed(1)}k`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)}k`;
  return String(Math.round(n));
}

/** Formats milliseconds into mobile-friendly elapsed time (e.g. 4.2s, 1m 12s, 1h 05m). */
export function formatDurationMs(ms?: number): string {
  const safe = Math.max(0, Math.round(ms || 0));
  const totalSec = Math.floor(safe / 1000);
  if (safe < 10_000) {
    return `${(safe / 1000).toFixed(1)}s`;
  }
  if (totalSec < 60) {
    return `${totalSec}s`;
  }
  const mins = Math.floor(totalSec / 60);
  const secs = totalSec % 60;
  if (mins < 60) {
    return `${mins}m ${String(secs).padStart(2, '0')}s`;
  }
  const hrs = Math.floor(mins / 60);
  const remMins = mins % 60;
  return `${hrs}h ${String(remMins).padStart(2, '0')}m`;
}

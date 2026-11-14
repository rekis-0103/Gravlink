import { useCallback, useEffect, useState } from 'react';

export type AlertKind = 'done' | 'approval';

function readStoredAlerts(): boolean {
  try {
    return localStorage.getItem('gravlink_alerts') === 'true';
  } catch {
    return false;
  }
}

function playChime(kind: AlertKind) {
  try {
    const Ctx =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctx) return;
    const ctx = new Ctx();
    const notes = kind === 'approval' ? [784, 988] : [523.25, 659.25, 783.99];
    const now = ctx.currentTime;
    notes.forEach((freq, idx) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'sine';
      osc.frequency.setValueAtTime(freq, now + idx * 0.09);
      gain.gain.setValueAtTime(0.08, now + idx * 0.09);
      gain.gain.exponentialRampToValueAtTime(0.001, now + idx * 0.09 + 0.14);
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(now + idx * 0.09);
      osc.stop(now + idx * 0.09 + 0.15);
    });
    window.setTimeout(() => void ctx.close().catch(() => {}), 700);
  } catch {
    // Ignore autoplay restriction or unsupported browser
  }
}

export function useAgentNotifications() {
  const [alertsEnabled, setAlertsEnabled] = useState(readStoredAlerts);

  useEffect(() => {
    try {
      localStorage.setItem('gravlink_alerts', String(alertsEnabled));
    } catch {
      // ignore
    }
  }, [alertsEnabled]);

  const toggleAlerts = useCallback(async () => {
    const next = !alertsEnabled;
    if (next) {
      if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
        try {
          await Notification.requestPermission();
        } catch {
          // Audio & vibrate alerts still work inside PWA/browser
        }
      }
      playChime('done');
    }
    setAlertsEnabled(next);
  }, [alertsEnabled]);

  const notify = useCallback(
    (title: string, body: string, kind: AlertKind) => {
      if (!alertsEnabled) return;
      playChime(kind);
      try {
        navigator.vibrate?.(kind === 'approval' ? [120, 60, 120] : [80]);
      } catch {
        // Ignore on iOS Safari
      }
      if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
        try {
          new Notification(title, { body, tag: `gravlink-${kind}` });
        } catch {
          // Ignore if mobile requires SW notification
        }
      }
    },
    [alertsEnabled]
  );

  return { alertsEnabled, toggleAlerts, notify };
}

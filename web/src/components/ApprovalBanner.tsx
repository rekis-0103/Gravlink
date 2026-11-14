import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { PendingInteraction } from '../types';

interface ApprovalBannerProps {
  conversationId: string;
  onResolved: () => void;
  onOpenHub: () => void;
  onPendingAppear?: (interaction: PendingInteraction) => void;
}

export function ApprovalBanner({
  conversationId,
  onResolved,
  onOpenHub,
  onPendingAppear,
}: ApprovalBannerProps) {
  const [pending, setPending] = useState<PendingInteraction[]>([]);
  const [busy, setBusy] = useState(false);
  const [confirmApprove, setConfirmApprove] = useState(false);
  const [errMsg, setErrMsg] = useState<string | null>(null);
  const seenKeysRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    setPending([]);
    setConfirmApprove(false);
    setErrMsg(null);
    seenKeysRef.current.clear();
    if (!conversationId) return;

    let cancelled = false;
    async function poll() {
      if (document.hidden) return;
      try {
        const res = await api.pending(conversationId);
        if (!cancelled) {
          const list = res.pending || [];
          setPending(list);
          for (const item of list) {
            const k = `${item.cascade_id}:${item.step_index}`;
            if (!seenKeysRef.current.has(k)) {
              seenKeysRef.current.add(k);
              onPendingAppear?.(item);
            }
          }
        }
      } catch {
        /* ignore transient polling failures */
      }
    }

    void poll();
    const timer = window.setInterval(() => void poll(), 1800);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [conversationId]);

  if (!pending.length) {
    return null;
  }

  const top = pending[0];
  const detailText =
    top.command_line || top.resource || top.prompt || top.tool_name || 'Confirmation needed';

  async function respond(allow: boolean) {
    if (allow && !confirmApprove) {
      setConfirmApprove(true);
      return;
    }
    setBusy(true);
    setErrMsg(null);
    try {
      await api.interact(conversationId, {
        trajectory_id: top.trajectory_id,
        step_index: top.step_index,
        kind: top.kind,
        allow,
        command_line: top.command_line,
      });
      setConfirmApprove(false);
      setPending((prev) => prev.slice(1));
      onResolved();
    } catch (err) {
      setErrMsg((err as Error).message || 'Action failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="approval-banner" role="alert" aria-live="assertive">
      <div className="approval-head">
        <span>
          ⚠ Approval Required ({top.kind === 'runCommand' ? 'Terminal' : top.kind})
          {pending.length > 1 ? ` · 1 of ${pending.length}` : ''}
        </span>
        <button type="button" className="compact" onClick={onOpenHub}>
          Open Hub
        </button>
      </div>

      <pre className="approval-cmd">{detailText}</pre>

      {errMsg && <div className="composer-error">{errMsg}</div>}

      <div className="approval-actions">
        <button
          type="button"
          className="btn-reject"
          disabled={busy}
          onClick={() => void respond(false)}
        >
          {busy ? 'Working…' : 'Reject'}
        </button>
        <button
          type="button"
          className="btn-approve"
          disabled={busy}
          onClick={() => void respond(true)}
        >
          {busy ? 'Sending…' : confirmApprove ? 'Confirm Approve ✓' : 'Approve'}
        </button>
      </div>
    </div>
  );
}

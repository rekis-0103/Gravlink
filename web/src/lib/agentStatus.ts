import type { TranscriptStep } from '../types';

const ACTIVE_STEP_STATUSES = new Set([
  'RUNNING',
  'PENDING',
  'GENERATING',
  'WAITING',
  'IN_PROGRESS',
  'STREAMING',
]);

export function isActiveStepStatus(status?: string): boolean {
  if (!status) return false;
  return ACTIVE_STEP_STATUSES.has(status.trim().toUpperCase());
}

/**
 * Determines whether the AI agent is actively running right now.
 * Historical stalled/error steps are ignored; only the latest step (or Hub status before steps exist) counts.
 */
export function detectAgentRunning(
  steps: TranscriptStep[],
  conversationStatus?: string
): { isRunning: boolean; activeStep: TranscriptStep | undefined } {
  if (steps.length > 0) {
    const last = steps[steps.length - 1];
    if (isActiveStepStatus(last.status)) {
      return { isRunning: true, activeStep: last };
    }
    return { isRunning: false, activeStep: undefined };
  }

  const normHub = (conversationStatus || '').trim().toUpperCase();
  return {
    isRunning: normHub.includes('RUNNING'),
    activeStep: undefined,
  };
}

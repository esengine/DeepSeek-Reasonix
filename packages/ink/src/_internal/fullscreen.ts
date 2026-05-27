import { isEnvTruthy } from './envUtils.js';

// Mouse tracking is a common pain point: it conflicts with terminal-level
// text selection (users can't copy output anymore) and breaks tmux's own
// mouse handling. The env var lets users opt out without code changes when
// they hit one of those scenarios.
export function isMouseClicksDisabled(): boolean {
  return isEnvTruthy(process.env['REASONIX_DISABLE_MOUSE']);
}

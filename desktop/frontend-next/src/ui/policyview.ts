import { t } from "../i18n";
import type { ApprovalMode, SessionStatus } from "../port/port";
import { approvalName } from "./approvals";

/** What the shelf says about how the next turn will run.
 *
 *  Fields rather than one joined string: only the dangerous part is toned, and
 *  a caller handed "均衡 · 全放行" cannot colour half of it. The not-ready arm
 *  carries no posture at all, so a screen cannot name one it has not been told.
 */
export type PolicySummary =
  | { ready: false }
  | { ready: true; preset?: string; effort?: string; approval?: string; danger?: true; reading: string };

/** Whether this turn departs from what a session runs as before anyone changes
 *  anything. Nothing does: there is nothing for the shelf to say, and a control
 *  reciting the defaults is what it says instead. */
export const deviates = (s: PolicySummary): boolean =>
  s.ready && !!(s.preset || s.effort || s.approval);

// What a session runs as before anyone changes anything — read off the kernel,
// not chosen here for being quiet. An unknown or retired preset normalises to
// balanced and a missing approval mode answers ask, so a session reporting
// these has not been configured rather than merely looking unconfigured.
const BASELINE_PRESET = "balanced";
const BASELINE_EFFORT = "auto";
const BASELINE_APPROVAL: ApprovalMode = "ask";

const presetName = (id: string): string =>
  ({ balanced: t("均衡"), delivery: t("交付") } as Record<string, string>)[id] ?? id;

// The kernel's own spelling, cased for the shelf. A display table would need a
// row for every rung an endpoint might publish, and the row it turns out to be
// missing is the one it renders blank.
const rung = (id: string): string => id.charAt(0).toUpperCase() + id.slice(1);


/** The collapsed projection of the three values that decide how a turn runs.
 *
 *  Baseline recedes and deviation surfaces: a shelf that recites the default
 *  posture forever spends attention on the turns where nothing is unusual, and
 *  has none left for the turn where something is. What is hidden is hidden
 *  because it is the value a session already has, never because it is long. */
export function policySummary(status: SessionStatus | null, hasEffort: boolean): PolicySummary {
  // Nothing authoritative has been read yet. Naming a posture from what the
  // kernel usually answers would be the context gauge's defect again — a
  // plausible value standing in for a fact — and this fact is what the agent
  // may do to a workspace without asking.
  if (!status) return { ready: false };

  const mode = status.toolApprovalMode;
  // A rung the endpoint does not publish is not a rung this session is on: the
  // open menu draws no ladder, and a summary claiming one would advertise a
  // control that is not there.
  const effort = hasEffort && status.effort ? status.effort : BASELINE_EFFORT;
  return {
    ready: true,
    preset: status.preset === BASELINE_PRESET ? undefined : presetName(status.preset),
    effort: effort === BASELINE_EFFORT ? undefined : rung(effort),
    // ask is the only mode that changes nothing about what happens without
    // being asked. auto lets some calls through unasked, dontAsk refuses what
    // it would have asked about, and yolo asks about nothing — none of those
    // is a preference the shelf may keep to itself.
    approval: mode === BASELINE_APPROVAL ? undefined : approvalName(mode),
    danger: mode === "yolo" || undefined,
    // The whole reading, for hover. Nothing is deleted by this projection; the
    // baseline values stop occupying the shelf and stay one pointer away.
    reading: [presetName(status.preset), hasEffort ? effort : "", approvalName(mode)].filter(Boolean).join(" · "),
  };
}

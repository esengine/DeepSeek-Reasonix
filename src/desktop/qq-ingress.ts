export type DesktopQQIngressDecision = "pause_reply" | "busy" | "new_turn";

export function classifyDesktopQQIngress(opts: {
  hasPendingInteraction: boolean;
  isBusy: boolean;
}): DesktopQQIngressDecision {
  if (opts.hasPendingInteraction) return "pause_reply";
  if (opts.isBusy) return "busy";
  return "new_turn";
}

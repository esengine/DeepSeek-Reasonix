import type { DictKey } from "./i18n";

export type MessageActionScope = "fork" | "fork-worktree" | "summ-from" | "summ-upto" | "conversation" | "code" | "both";
export type MessageActionState = { turn: number; scope: MessageActionScope };

export function messageActionLabelKey(scope: MessageActionScope, confirming: boolean): DictKey {
  switch (scope) {
    case "fork-worktree": return confirming ? "rewind.confirmForkWorktree" : "rewind.forkWorktree";
    case "fork": return confirming ? "rewind.confirmFork" : "rewind.forkConversation";
    case "summ-from": return confirming ? "rewind.confirmSummFrom" : "rewind.summFrom";
    case "summ-upto": return confirming ? "rewind.confirmSummUpto" : "rewind.summUpto";
    case "conversation": return confirming ? "rewind.confirmConversation" : "rewind.conversation";
    case "code": return confirming ? "rewind.confirmCode" : "rewind.code";
    default: return confirming ? "rewind.confirmBoth" : "rewind.both";
  }
}

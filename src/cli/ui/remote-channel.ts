import type { CheckpointMeta } from "../../code/checkpoints.js";
import type { SessionInfo } from "../../memory/session.js";
import type { SlashResult } from "./slash/types.js";

export interface RemoteSlashHandlingArgs {
  result: SlashResult;
  codeMode: boolean;
  sessions: SessionInfo[];
  checkpoints: CheckpointMeta[];
  models: string[] | null | undefined;
  restoreCodeOnlyMessage: string;
}

export interface RemoteChannelLike {
  sendText: (message: string) => void;
  sendInfo: (message: string) => void;
  handleRemoteSlashResult: (args: RemoteSlashHandlingArgs) => boolean;
}

export function pickRemoteChannel(
  source: "qq" | "feishu" | null,
  qq: RemoteChannelLike,
  feishu: RemoteChannelLike,
): RemoteChannelLike | null {
  if (source === "qq") return qq;
  if (source === "feishu") return feishu;
  return null;
}

export function relayRemoteSlashInfo(channel: RemoteChannelLike | null, info: string | undefined) {
  if (channel && info) channel.sendText(info);
}

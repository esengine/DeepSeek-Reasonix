import type { SlashHandler } from "../dispatch.js";

const sessions: SlashHandler = () => ({ openSessionsPicker: true });

export const handlers: Record<string, SlashHandler> = {
  sessions,
};

import { t } from "@/i18n/index.js";
import { deleteSession, listSessions, renameSession } from "@/memory/session.js";
import type { SlashHandler } from "../dispatch.js";

const sessions: SlashHandler = () => ({ openSessionsPicker: true });

const forget: SlashHandler = (_args, loop) => {
  if (!loop.sessionName) {
    return { info: t("handlers.sessions.forgetNoSession") };
  }
  const name = loop.sessionName;
  const ok = deleteSession(name);
  return {
    info: ok
      ? t("handlers.sessions.forgetInfo", { name })
      : t("handlers.sessions.forgetFailed", { name }),
  };
};

const rename: SlashHandler = (args, loop) => {
  const newName = args?.[0]?.trim();
  if (!newName) return { info: t("handlers.sessions.renameUsage") };
  if (!loop.sessionName) return { info: t("handlers.sessions.renameNoSession") };
  const ok = renameSession(loop.sessionName, newName);
  if (!ok) {
    return { info: t("handlers.sessions.renameFailed", { name: newName }) };
  }
  return { info: t("handlers.sessions.renameInfo", { name: newName }) };
};

const resume: SlashHandler = (args) => {
  const name = args?.[0]?.trim();
  if (!name) return { info: t("handlers.sessions.resumeUsage") };
  const exists = listSessions().some((s) => s.name === name);
  if (!exists) return { info: t("handlers.sessions.resumeNotFound", { name }) };
  return { info: t("handlers.sessions.resumeInfo", { name }) };
};

export const handlers: Record<string, SlashHandler> = {
  sessions,
  forget,
  rename,
  resume,
};

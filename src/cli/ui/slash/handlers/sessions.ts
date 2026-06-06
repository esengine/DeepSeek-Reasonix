import { readConfig, writeConfig } from "@/config.js";
import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

const sessions: SlashHandler = () => ({ openSessionsPicker: true });

const title: SlashHandler = (_args, _loop, ctx) => {
  if (!ctx.generateSessionTitle || !ctx.postInfo) {
    return { info: t("handlers.sessions.titleUnavailable") };
  }
  void ctx.generateSessionTitle().then(
    (info) => ctx.postInfo?.(info),
    (err) =>
      ctx.postInfo?.(
        t("handlers.sessions.titleFailed", {
          reason: err instanceof Error ? err.message : String(err),
        }),
      ),
  );
  return { info: t("handlers.sessions.titleStarted") };
};

/** `/session persist on|off` — toggle whether `reasonix code/chat` resumes
 *  the previous session on launch (#2238). Persists to config.json. */
const persist: SlashHandler = (args, _loop, ctx) => {
  const sub = (args[0] ?? "").toLowerCase();
  const cfg = readConfig(ctx.configPath);
  const current = cfg.autoResumeSession !== false;
  if (sub === "") {
    return {
      info: current ? t("handlers.sessions.persistOn") : t("handlers.sessions.persistOff"),
    };
  }
  const next = sub === "on" || sub === "true" || sub === "1";
  const off = sub === "off" || sub === "false" || sub === "0";
  if (!next && !off) {
    return { info: t("handlers.sessions.persistUsage") };
  }
  cfg.autoResumeSession = next;
  try {
    writeConfig(cfg, ctx.configPath);
  } catch {
    /* disk full / perms — runtime change still noted */
  }
  return {
    info: next ? t("handlers.sessions.persistSetOn") : t("handlers.sessions.persistSetOff"),
  };
};

export const handlers: Record<string, SlashHandler> = {
  sessions,
  title,
  "session-persist": persist,
};

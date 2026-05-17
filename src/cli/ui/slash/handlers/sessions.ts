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

export const handlers: Record<string, SlashHandler> = {
  sessions,
  title,
};

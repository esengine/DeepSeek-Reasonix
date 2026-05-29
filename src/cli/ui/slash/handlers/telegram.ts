import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  telegram(args, _loop, ctx) {
    const subcommand = (args[0] ?? "status").toLowerCase();
    if (!ctx.telegram) {
      return { info: t("handlers.telegram.unavailable") };
    }

    if (subcommand === "connect") {
      ctx.postInfo?.(t("handlers.telegram.connecting"));
      void ctx.telegram.connect(args.slice(1)).then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(
            t("handlers.telegram.connectFailed", {
              reason: (err as Error).message,
            }),
          ),
      );
      return {};
    }

    if (subcommand === "disconnect") {
      ctx.postInfo?.(t("handlers.telegram.disconnecting"));
      void ctx.telegram.disconnect().then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(
            t("handlers.telegram.disconnectFailed", {
              reason: (err as Error).message,
            }),
          ),
      );
      return {};
    }

    if (subcommand === "status") {
      return { info: ctx.telegram.status() };
    }

    return {
      info: t("handlers.telegram.usage"),
    };
  },
};

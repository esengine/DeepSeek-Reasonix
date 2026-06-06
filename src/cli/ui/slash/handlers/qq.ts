import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  qq(args, _loop, ctx) {
    const subcommand = (args[0] ?? "status").toLowerCase();
    if (!ctx.qq) {
      return { info: t("handlers.qq.unavailable") };
    }

    if (subcommand === "connect") {
      ctx.postInfo?.(t("handlers.qq.connecting"));
      void ctx.qq.connect(args.slice(1)).then(
        (message) => ctx.postInfo?.(message),
        (err) => ctx.postInfo?.(t("handlers.qq.connectFailed", { reason: (err as Error).message })),
      );
      return {};
    }

    if (subcommand === "disconnect") {
      ctx.postInfo?.(t("handlers.qq.disconnecting"));
      void ctx.qq.disconnect().then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(t("handlers.qq.disconnectFailed", { reason: (err as Error).message })),
      );
      return {};
    }

    if (subcommand === "status") {
      return { info: ctx.qq.status() };
    }

    return {
      info: t("handlers.qq.usage"),
    };
  },
};

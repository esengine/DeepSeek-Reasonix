import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  feishu(args, _loop, ctx) {
    const subcommand = (args[0] ?? "status").toLowerCase();
    if (!ctx.feishu) {
      return { info: t("handlers.feishu.unavailable") };
    }

    if (subcommand === "connect") {
      ctx.postInfo?.(t("handlers.feishu.connecting"));
      void ctx.feishu.connect(args.slice(1)).then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(t("handlers.feishu.connectFailed", { reason: (err as Error).message })),
      );
      return {};
    }

    if (subcommand === "disconnect") {
      ctx.postInfo?.(t("handlers.feishu.disconnecting"));
      void ctx.feishu.disconnect().then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(t("handlers.feishu.disconnectFailed", { reason: (err as Error).message })),
      );
      return {};
    }

    if (subcommand === "status") {
      return { info: ctx.feishu.status() };
    }

    return {
      info: t("handlers.feishu.usage"),
    };
  },
};

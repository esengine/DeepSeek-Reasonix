import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  weixin(args, _loop, ctx) {
    const subcommand = (args[0] ?? "status").toLowerCase();
    if (!ctx.weixin) {
      return { info: t("handlers.weixin.unavailable") };
    }

    if (subcommand === "connect") {
      ctx.postInfo?.(t("handlers.weixin.connecting"));
      void ctx.weixin.connect(args.slice(1)).then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(
            t("handlers.weixin.connectFailed", {
              reason: (err as Error).message,
            }),
          ),
      );
      return {};
    }

    if (subcommand === "disconnect") {
      ctx.postInfo?.(t("handlers.weixin.disconnecting"));
      void ctx.weixin.disconnect().then(
        (message) => ctx.postInfo?.(message),
        (err) =>
          ctx.postInfo?.(
            t("handlers.weixin.disconnectFailed", {
              reason: (err as Error).message,
            }),
          ),
      );
      return {};
    }

    if (subcommand === "status") {
      return { info: ctx.weixin.status() };
    }

    return { info: t("handlers.weixin.usage") };
  },
};

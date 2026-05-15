import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  qq(args, _loop, ctx) {
    const subcommand = (args[0] ?? "status").toLowerCase();
    const rest = args.slice(1);
    if (!ctx.qq) {
      return { info: "/qq is not available in this session." };
    }

    if (subcommand === "connect") {
      ctx.postInfo?.("QQ: connecting...");
      void ctx.qq.connect(rest).then(
        (message) => ctx.postInfo?.(message),
        (err) => ctx.postInfo?.(`QQ connect failed: ${(err as Error).message}`),
      );
      return {};
    }

    if (subcommand === "disconnect") {
      ctx.postInfo?.("QQ: disconnecting...");
      void ctx.qq.disconnect().then(
        (message) => ctx.postInfo?.(message),
        (err) => ctx.postInfo?.(`QQ disconnect failed: ${(err as Error).message}`),
      );
      return {};
    }

    if (subcommand === "status") {
      return { info: ctx.qq.status() };
    }

    return { info: "Usage: /qq connect [appId appSecret [sandbox]] | /qq status | /qq disconnect" };
  },
};

import { DEEPSEEK_PRICING } from "../../telemetry/stats.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

export async function handleModels(
  method: string,
  _rest: string[],
  _body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method !== "GET") return { status: 405, body: { error: "GET only" } };
  const models = ctx.getModels?.() ?? null;
  return {
    status: 200,
    body: {
      models,
      current: ctx.loop?.model ?? null,
      /** USD per 1M tokens — same table the cost gauge uses. */
      pricing: DEEPSEEK_PRICING,
    },
  };
}

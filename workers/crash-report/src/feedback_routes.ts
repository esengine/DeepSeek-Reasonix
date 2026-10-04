import type { Env } from "./env";
import { handleAdmin } from "./feedback_admin";
import { ATTACHMENT_ROUTE, serveAttachment } from "./feedback_attachments";
import { refuse } from "./feedback_http";
import { handleMine } from "./feedback_read";
import { handleUserReply } from "./feedback_reply";
import { servePage } from "./feedback_admin_page";
import type { OpsWaiter } from "./ops_emit";
import { handleSubmit } from "./feedback_submit";

// Returns null when the path is not a feedback route so the caller keeps routing.
export async function handleFeedbackRoute(request: Request, env: Env, ctx?: OpsWaiter): Promise<Response | null> {
  const url = new URL(request.url);
  const path = url.pathname;
  const method = request.method;
  const page = servePage(request, path);
  if (page) return page;
  if (path === "/v1/feedback") return method === "POST" ? handleSubmit(request, env, ctx) : refuse("feedback.method_not_allowed", "method not allowed");
  if (path === "/v1/feedback/mine") return method === "GET" ? handleMine(request, env) : refuse("feedback.method_not_allowed", "method not allowed");
  const reply = path.match(/^\/v1\/feedback\/(FB-[0-9A-Z]{4}-[0-9A-Z]{4})\/reply$/);
  if (reply) return method === "POST" ? handleUserReply(request, env, reply[1], ctx) : refuse("feedback.method_not_allowed", "method not allowed");
  if (path.startsWith(ATTACHMENT_ROUTE)) {
    return method === "GET" ? serveAttachment(env, path.slice(ATTACHMENT_ROUTE.length)) : refuse("feedback.method_not_allowed", "method not allowed");
  }
  return handleAdmin(request, env, url, ctx);
}

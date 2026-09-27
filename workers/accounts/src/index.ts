// Reasonix account service — email/password auth, sessions, and public profiles
// for reasonix.io, plus bounded retention for its expiring authorization state.
import app from "./app";
import type { Bindings } from "./env";
import { purgeExpiredAuthState } from "./maintenance";

export default {
  async fetch(request: Request, env: Bindings, ctx: ExecutionContext): Promise<Response> {
    return app.fetch(request, env, ctx);
  },

  scheduled(_controller: ScheduledController, env: Bindings, ctx: ExecutionContext): void {
    ctx.waitUntil(
      purgeExpiredAuthState(env).then((purged) => {
        console.log(`auth retention: purged ${purged} expired rows`);
      }),
    );
  },
};

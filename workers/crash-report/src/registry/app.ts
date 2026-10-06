import { Hono } from "hono";
import type { AppEnv } from "./env";
import { corsMiddleware } from "./http/cors";
import { noStoreByDefault } from "./http/cache";
import { errorHandler, notFoundHandler } from "./http/errors";
import health from "./routes/health";
import packages from "./routes/packages";
import engagement from "./routes/engagement";
import activity from "./routes/activity";
import admin from "./routes/admin";
import me from "./routes/me";

const app = new Hono<AppEnv>();

app.onError(errorHandler);
app.notFound(notFoundHandler);

app.use("*", corsMiddleware);
app.use("*", noStoreByDefault);

app.route("/", health);
app.route("/v1/packages", packages);
app.route("/v1/packages", engagement);
app.route("/v1/activity", activity);
app.route("/v1/admin", admin);
app.route("/v1/me", me);

export default app;

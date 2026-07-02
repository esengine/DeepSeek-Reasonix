import { Hono } from "hono";
import type { AppEnv } from "./env";
import { corsMiddleware } from "./http/cors";
import { errorHandler, notFoundHandler } from "./http/errors";
import health from "./routes/health";
import packages from "./routes/packages";
import activity from "./routes/activity";
import admin from "./routes/admin";

const app = new Hono<AppEnv>();

app.onError(errorHandler);
app.notFound(notFoundHandler);

app.use("*", corsMiddleware);

app.route("/", health);
app.route("/v1/packages", packages);
app.route("/v1/activity", activity);
app.route("/v1/admin", admin);

export default app;

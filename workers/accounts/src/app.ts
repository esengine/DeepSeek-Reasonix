import { Hono } from "hono";
import type { AppEnv } from "./env";
import { corsMiddleware } from "./http/cors";
import { loadUser } from "./http/auth";
import { errorHandler, notFoundHandler } from "./http/errors";
import health from "./routes/health";
import auth from "./routes/auth";
import device from "./routes/device";
import me from "./routes/me";
import users from "./routes/users";

const app = new Hono<AppEnv>();

app.onError(errorHandler);
app.notFound(notFoundHandler);

app.use("*", corsMiddleware);
app.use("*", loadUser);

app.route("/", health);
app.route("/auth", auth);
app.route("/device", device);
app.route("/me", me);
app.route("/u", users);

export default app;

import { Hono } from "hono";
import type { AppEnv } from "../env";

const health = new Hono<AppEnv>();

health.get("/", (c) => c.json({ ok: true, service: "accounts" }));
health.get("/health", (c) => c.json({ ok: true, service: "accounts" }));

export default health;

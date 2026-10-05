import { Hono } from "hono";
import type { AppEnv } from "../env";

const health = new Hono<AppEnv>();

health.get("/health", (c) => c.json({ ok: true, service: "reasonix-registry" }));

export default health;

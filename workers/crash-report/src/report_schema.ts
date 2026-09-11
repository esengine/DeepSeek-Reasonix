import { z } from "zod";

const technicalString = (max: number) => z.string().max(max).refine((value) => !/[\u0000-\u001f\u007f]/.test(value));
const runtimeBucket = z.string().trim().min(1).max(64).regex(/^[a-z0-9_]+$/);

const Device = z.object({
  osVersion: technicalString(128),
  osBuild: z.number().int().min(0).max(1_000_000),
  osRevision: z.number().int().min(0).max(1_000_000),
  distroId: technicalString(64),
  distroVersion: technicalString(64),
  kernelVersion: technicalString(128),
  sessionType: z.enum(["wayland", "x11", "remote", "unknown"]),
  cpu: technicalString(128),
  cores: z.number().int().min(0).max(4096),
  ramGb: z.number().min(0).max(65536),
}).partial();

const WebView2Diagnostic = z.object({
  kind: runtimeBucket,
  reason: runtimeBucket,
  exitCode: z.number().int().min(-2147483648).max(2147483647).optional(),
  processDescription: technicalString(255).optional(),
  failureSourceModule: z.string().max(255).refine((value) => !/[\\/]/.test(value)).optional(),
  runtimeVersion: technicalString(128),
  gpuDisabled: z.boolean(),
  recovery: z.enum(["not_applicable", "reload_succeeded", "reload_failed"]),
});

export const WebRuntimeDiagnostic = z.object({
  engine: z.enum(["webview2", "webkitgtk"]),
  kind: runtimeBucket,
  reason: runtimeBucket,
  exitCode: z.number().int().min(-2147483648).max(2147483647).optional(),
  processDescription: technicalString(255).optional(),
  failureSourceModule: technicalString(255).optional(),
  runtimeVersion: technicalString(128),
  gpuMode: z.enum(["enabled", "disabled", "always", "on_demand", "unknown"]),
  recovery: z.enum(["not_applicable", "reload_succeeded", "reload_failed"]),
});

export const Report = z.object({
  eventId: z.string().regex(/^[0-9a-f]{32}$/).optional(),
  dedupKey: z.string().regex(/^[0-9a-f]{64}$/).optional(),
  installId: z.string().regex(/^[0-9a-f]{32}$/).optional(),
  kind: z.enum(["crash", "exception", "feedback", "performance", "bot"]),
  version: z.string().min(1).max(64),
  os: z.string().min(1).max(32),
  arch: z.string().min(1).max(32),
  message: z.string().min(1).max(16 * 1024),
  device: Device.optional(),
  schemaVersion: z.number().int().min(1).max(10).optional(),
  source: z.string().trim().min(1).max(32).regex(/^[a-z0-9_.-]+$/).optional(),
  label: z.string().max(64).optional(),
  errorType: z.string().max(128).optional(),
  errorMessage: z.string().max(4 * 1024).optional(),
  stack: z.string().max(16 * 1024).optional(),
  componentStack: z.string().max(16 * 1024).optional(),
  topFrame: z.string().max(300).optional(),
  fingerprintHint: z.string().max(300).optional(),
  buildCommit: z.string().max(64).optional(),
  channel: z.string().max(32).optional(),
  language: z.string().max(64).optional(),
  view: z.string().max(200).optional(),
  breadcrumbs: z.array(z.object({
    t: z.number().int().optional(),
    cat: z.string().max(64).optional(),
    msg: z.string().max(240).optional(),
  })).max(30).optional(),
  occurredAt: z.string().max(64).optional(),
  webRuntime: WebRuntimeDiagnostic.optional(),
  webview2: WebView2Diagnostic.optional(),
});

export type ReportPayload = z.infer<typeof Report>;

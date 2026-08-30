// Run: tsx src/__tests__/mcp-interaction.test.tsx
// MCP elicitation: wire event → reducer state, StructuredForm schema
// normalization/coercion, MCPInteractionCard rendering and answer wiring.

import { JSDOM } from "jsdom";
import { readFileSync } from "node:fs";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css") || specifier.endsWith(".svg")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

import { LocaleProvider } from "../lib/i18n";
import type { WireEvent } from "../lib/types";
import { initialState, reducer } from "../lib/useController";
import { app, onEvent } from "../lib/bridge";
import { DECISION_SURFACE_MOCK_TRIGGERS } from "../lib/decisionSurfaceMock";
import {
  coerceStructuredValues,
  initialStructuredValues,
  missingStructuredRequired,
  normalizeStructuredSchema,
  parseStructuredSchema,
} from "../components/StructuredForm";

const { MCPInteractionCard } = await import("../components/MCPInteractionCard");

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

type ControllerState = Parameters<typeof reducer>[0];

const appSource = readFileSync(new URL("../App.tsx", import.meta.url), "utf8");
ok(
  /\[clearContextPending, pendingClose, state\.approval, state\.ask, state\.extensionForm, state\.mcpInteraction, workspaceConflict\]/.test(appSource),
  "App decision surface recomputes when an MCP interaction arrives",
);

const dom = new JSDOM("<!doctype html><html><body><div id='root'></div></body></html>");
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
(dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void; detachEvent: () => void }).attachEvent = () => {};
(dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void; detachEvent: () => void }).detachEvent = () => {};

// ── Schema normalization ─────────────────────────────────────────────────────

{
  const fields = normalizeStructuredSchema({
    type: "object",
    required: ["code", "count"],
    properties: {
      code: { type: "string", title: "Device code", minLength: 4, maxLength: 8 },
      count: { type: "integer", minimum: 1, maximum: 5, default: 2 },
      ok: { type: "boolean" },
      flavor: { enum: ["vanilla", "mint"], enumNames: ["Vanilla bean", "Fresh mint"] },
      region: { type: "string", oneOf: [{ const: "us", title: "United States" }, { const: "sg", title: "Singapore" }] },
      scopes: {
        type: "array",
        items: { anyOf: [{ const: "read", title: "Read data" }, { const: "write", title: "Write data" }] },
        minItems: 1,
        maxItems: 2,
        default: ["read"],
      },
      email: { type: "string", format: "email" },
    },
  });
  ok(fields.length === 7, "old and new flat schema properties become fields");
  const code = fields.find((f) => f.key === "code");
  ok(code?.kind === "string" && code.required && code.minLength === 4, "string field carries required + bounds");
  const count = fields.find((f) => f.key === "count");
  ok(count?.kind === "integer" && count.defaultValue === 2 && count.maximum === 5, "integer field carries default + max");
  ok(fields.find((f) => f.key === "ok")?.kind === "boolean", "boolean field detected");
  ok(fields.find((f) => f.key === "flavor")?.options?.[0].label === "Vanilla bean", "legacy enumNames labels stay compatible");
  ok(fields.find((f) => f.key === "region")?.options?.[1].label === "Singapore", "2025-11-25 titled single-select is normalized");
  const scopes = fields.find((f) => f.key === "scopes");
  ok(scopes?.kind === "multi-enum" && Array.isArray(scopes.defaultValue) && scopes.defaultValue[0] === "read", "2025-11-25 titled multi-select carries defaults");
  ok(fields.find((f) => f.key === "email")?.format === "email", "string formats reach the renderer");
  const nested = parseStructuredSchema({ type: "object", properties: { account: { type: "object" } } });
  ok(nested.unsupported && nested.fields[0]?.kind === "unsupported", "nested schemas fail closed instead of becoming text");
  ok(normalizeStructuredSchema(null).length === 0, "null schema yields no fields");
  ok(normalizeStructuredSchema({}).length === 0, "schema without properties yields no fields");
}

// ── Value coercion ───────────────────────────────────────────────────────────

{
  const fields = normalizeStructuredSchema({
    type: "object",
    required: ["name"],
    properties: {
      name: { type: "string", minLength: 2 },
      age: { type: "integer" },
      pi: { type: "number" },
      ok: { type: "boolean", default: true },
      roles: { type: "array", items: { type: "string", enum: ["reader", "writer"] }, minItems: 1 },
    },
  });
  const defaults = initialStructuredValues(fields);
  ok(defaults.ok === true, "boolean default stays explicit");
  ok(missingStructuredRequired(fields, defaults)[0] === "name", "missing required reported by label");
  const { content, invalid } = coerceStructuredValues(fields, {
    ...defaults,
    name: "jo",
    age: "41",
    pi: "3.5",
    roles: ["reader"],
  });
  ok(invalid.length === 0, "valid values coerce without errors");
  ok(content.name === "jo" && content.age === 41 && content.pi === 3.5 && content.ok === true, "scalar values coerce to JSON types");
  ok(Array.isArray(content.roles) && content.roles[0] === "reader", "multi-select answers remain string arrays");
  const bad = coerceStructuredValues(fields, { ...defaults, name: "j", age: "4.2", roles: [] });
  ok(bad.invalid.includes("name") && bad.invalid.includes("age") && bad.invalid.includes("roles"), "bounds, fractional integers, and selection limits are rejected");
}

// ── Reducer ──────────────────────────────────────────────────────────────────

{
  const event: WireEvent = {
    kind: "mcp_interaction",
    turnId: "t1",
    itemId: "42",
    mcpInteraction: {
      id: "42",
      server: "github",
      mode: "form",
      message: "confirm",
      requestedSchema: { type: "object", properties: { code: { type: "string" } }, required: ["code"] },
    },
  } as unknown as WireEvent;
  const next = reducer({ ...initialState }, { type: "event", e: event } as never);
  ok((next as ControllerState).mcpInteraction?.id === "42", "mcp_interaction event sets state");
  ok((next as ControllerState).pendingPrompt === true, "mcp_interaction waits for the user");

  const answered = reducer(next, {
    type: "event",
    e: { kind: "prompt_answered", turnId: "t1", itemId: "42" } as unknown as WireEvent,
  } as never);
  ok((answered as ControllerState).mcpInteraction === undefined, "prompt_answered clears the card");
}

// ── Browser mock lifecycle ──────────────────────────────────────────────────

{
  const events: WireEvent[] = [];
  const unsubscribe = onEvent((event) => events.push(event));
  await app.SubmitToTabWithID("mock-mcp-tab", DECISION_SURFACE_MOCK_TRIGGERS.mcp_interaction, "mock-mcp-submission");
  const interaction = events.find((event) => event.kind === "mcp_interaction");
  ok(interaction?.tabId === "mock-mcp-tab", "browser mock routes the MCP interaction to its origin tab");
  ok(interaction?.mcpInteraction?.server === "github", "browser mock identifies the requesting MCP server");
  const schema = interaction?.mcpInteraction?.requestedSchema as { properties?: Record<string, unknown> } | undefined;
  ok(Object.keys(schema?.properties ?? {}).join(",") === "code,environment,permissions,remember", "browser mock exposes old and new structured controls");
  await app.AnswerMCPInteractionForTab("mock-mcp-tab", interaction?.mcpInteraction?.id ?? "", "accept", {
    code: "123-456",
    environment: "staging",
    permissions: ["repo:read"],
    remember: false,
  });
  ok(events.some((event) => event.kind === "prompt_answered" && event.tabId === "mock-mcp-tab"), "browser mock acknowledges the MCP answer on the origin tab");
  ok(events.some((event) => event.kind === "turn_done" && event.tabId === "mock-mcp-tab"), "browser mock completes the preview turn after an answer");
  await app.SubmitToTabWithID("mock-mcp-tab", DECISION_SURFACE_MOCK_TRIGGERS.mcp_interaction, "mock-mcp-submission-2");
  const interactions = events.filter((event) => event.kind === "mcp_interaction");
  ok(interactions.length === 2, "browser mock can be triggered repeatedly");
  ok(interactions[0].mcpInteraction?.id !== interactions[1].mcpInteraction?.id, "repeated browser mocks use distinct prompt ids");
  await app.AnswerMCPInteractionForTab("mock-mcp-tab", interactions[1].mcpInteraction?.id ?? "", "cancel", null);
  unsubscribe();
}

// ── Card rendering + submit wiring ───────────────────────────────────────────

{
  const answers: { id: string; action: string; content?: Record<string, unknown> }[] = [];
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <MCPInteractionCard
          interaction={{
            id: "7",
            server: "github",
            mode: "form",
            message: "Enter the device code",
            requestedSchema: {
              type: "object",
              required: ["code"],
              properties: { code: { type: "string", title: "Device code", default: "123-456" } },
            },
          }}
          busy={false}
          onAnswer={(id, action, content) => answers.push({ id, action, content })}
        />
      </LocaleProvider>,
    );
  });
  const text = document.body.textContent ?? "";
  ok(text.includes("github") && text.includes("Device code"), "card shows server and field label");
  const dialog = document.querySelector("[role='dialog']");
  const descriptionID = dialog?.getAttribute("aria-describedby") ?? "";
  ok(Boolean(descriptionID) && document.getElementById(descriptionID)?.textContent === "Enter the device code", "server message describes the dialog without crowding its title");

  const input = document.querySelector(".structured-form-control") as HTMLInputElement | null;
  ok(input !== null, "form field rendered as input");
  // jsdom cannot reliably drive React controlled-input keystrokes; the default
  // value prefills the field so the accept path is exercised end-to-end, and
  // typed-value coercion is covered above.
  ok(input?.value === "123-456", "required field prefilled from schema default");
  ok(document.activeElement === input, "new form focuses its first field");
  ok(document.querySelectorAll("[role='option']").length === 0, "immediate MCP actions are not exposed as listbox options");
  ok(document.querySelector(".prompt-shelf-bar-actions")?.getAttribute("role") === "group", "MCP actions expose button-group semantics");
  const submit = Array.from(document.querySelectorAll("button, [role='button']")).find((b) =>
    (b.textContent ?? "").toLowerCase().includes("submit"),
  );
  ok(submit !== undefined, "submit action rendered");
  if (submit) {
    await act(async () => {
      submit.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
  }

  ok(
    answers.length === 1 && answers[0].action === "accept" && (answers[0].content as Record<string, unknown>)?.code === "123-456",
    "submit sends accept with the typed form values",
  );
  await act(async () => {
    root.unmount();
  });
}

// ── Unsupported schema fallback ─────────────────────────────────────────────

{
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <MCPInteractionCard
          interaction={{
            id: "unsupported",
            server: "future-server",
            mode: "form",
            message: "Provide account details",
            requestedSchema: { type: "object", properties: { account: { type: "object" } } },
          }}
          busy={false}
          onAnswer={() => {}}
        />
      </LocaleProvider>,
    );
  });
  const submit = Array.from(document.querySelectorAll("button")).find((button) => button.textContent === "Submit");
  const cancel = Array.from(document.querySelectorAll("button")).find((button) => button.textContent === "Cancel");
  ok(submit?.disabled === true, "unknown nested schema cannot be submitted as lossy text");
  ok(cancel?.disabled === false && Boolean(document.querySelector("[role='alert']")), "unsupported form keeps a clear safe exit and explanation");
  await act(async () => root.unmount());
}

// ── Required-field validation ───────────────────────────────────────────────

{
  const answers: string[] = [];
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <MCPInteractionCard
          interaction={{
            id: "8",
            server: "calendar-with-a-deliberately-long-server-name",
            mode: "form",
            message: "Please provide the account identifier used for this calendar connection.",
            requestedSchema: {
              type: "object",
              required: ["account"],
              properties: { account: { type: "string", title: "Account email", format: "email" } },
            },
          }}
          busy={false}
          onAnswer={(_id, action) => answers.push(action)}
        />
      </LocaleProvider>,
    );
  });
  const submit = Array.from(document.querySelectorAll("button")).find((button) => button.textContent === "Submit");
  await act(async () => submit?.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  const input = document.querySelector(".structured-form-control") as HTMLInputElement | null;
  ok(answers.length === 0, "invalid form does not answer the blocked MCP request");
  ok(input?.required === true && input.getAttribute("aria-invalid") === "true", "required field exposes native and ARIA validation state");
  ok(Boolean(input?.getAttribute("aria-describedby")), "field error is associated with its control");
  ok(document.activeElement === input, "failed submit returns focus to the first invalid field");
  await act(async () => root.unmount());
}

// ── URL card ─────────────────────────────────────────────────────────────────

{
  const answers: { id: string; action: string; content?: Record<string, unknown> }[] = [];
  const opened: string[] = [];
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <MCPInteractionCard
          interaction={{
            id: "9",
            server: "linear",
            mode: "url",
            message: "Finish sign-in",
            url: "https://auth.example.com/cb?state=xyz",
          }}
          busy={false}
          onAnswer={(id, action, content) => answers.push({ id, action, content })}
          onOpenLink={(url) => opened.push(url)}
        />
      </LocaleProvider>,
    );
  });
  const text = document.body.textContent ?? "";
  const urlServer = document.querySelector(".mcp-interaction-url > span:first-child")?.textContent;
  const urlHost = document.querySelector(".mcp-interaction-url > strong")?.textContent;
  ok(urlServer === "linear" && urlHost === "auth.example.com", "url card shows the exact server and target host");
  ok(!text.includes("?state=xyz"), "url card hides query params from the summary line");
  const urlButtons = Array.from(document.querySelectorAll(".prompt-shelf-bar-actions button"));
  const open = urlButtons.find((button) => (button.textContent ?? "").startsWith("Open "));
  ok(urlButtons.some((button) => button.textContent === "Cancel"), "url mode keeps cancel distinct from decline");
  if (open) {
    await act(async () => {
      open.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
  }
  ok(opened.length === 1 && opened[0] === "https://auth.example.com/cb?state=xyz", "open link passes the exact URL once");
  const accept = Array.from(document.querySelectorAll("button, [role='button']")).find((b) =>
    (b.textContent ?? "").toLowerCase() === "accept",
  );
  if (accept) {
    await act(async () => {
      accept.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
  }
  ok(answers.length === 1 && answers[0].action === "accept" && answers[0].content === undefined, "accept without form content");
  await act(async () => {
    root.unmount();
  });
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);

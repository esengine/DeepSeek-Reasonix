// provider.ts — a model source as the connection panel edits it: what was
// probed, what was confirmed, and the few fields no probe can answer.

export interface ProviderEntry {
  http1Only?: boolean;
  name: string;
  // The label a person gave this source. Only ever shown: every ref, the
  // default model and saved sessions keep pointing at name.
  displayName?: string;
  kind: string;
  baseUrl: string;
  models: string[];
  default: string;
  hasKey: boolean;
  keyEnv?: string;
  // Which of them read images, so an editor shows the current answer rather
  // than asking the user to remember it.
  visionModels?: string[];
  // False where the kernel refuses image input for every model on this endpoint
  // regardless of config, so an editor can say so instead of offering a dead
  // switch. visionSettable narrows it to the models the refusal spares — one
  // endpoint serves text-only and image-taking models side by side.
  canSetVision?: boolean;
  visionSettable?: string[];
  // The endpoint-executed search tool. canWebSearch says this door offers one
  // at all; webSearch whether it is on. They differ between an account's doors.
  canWebSearch?: boolean;
  webSearch?: boolean;
  // Whether thinking/reasoning_effort may go on the wire. canSetThinking is
  // false where the protocol never carries them, so the switch appears only
  // where a relay can actually reject the request over it.
  canSetThinking?: boolean;
  sendsThinking?: boolean;
  // How this endpoint carries context between turns, and whether its protocol
  // has the choice at all. "" is vendor detection, which is what an endpoint
  // nobody has characterised keeps doing.
  canSetContinuation?: boolean;
  continuation?: string;
  // Which request shape this endpoint controls thinking with, as declared.
  // Absent is "nobody said", which is why a relay's effort ladder comes out
  // empty — nothing can be probed for it.
  reasoningProtocol?: string;
  // A declared effort vocabulary, which replaces the protocol's ladder, and the
  // level auto resolves to. Absent is no declaration.
  supportedEfforts?: string[];
  defaultEffort?: string;
  // Per model, for a relay serving several vendors under one key: what a model
  // declares for itself, the ladder it inherits when it declares nothing, and
  // a reasoning protocol it declares for itself.
  modelEfforts?: Record<string, ModelEffort>;
  inheritedEfforts?: Record<string, ModelEffort>;
  modelProtocols?: Record<string, string>;
  // Removing the one in use would leave the session on a model that no longer
  // resolves, so the row offers no delete.
  inUse: boolean;
  preset: boolean;
  // The three no probe can answer: the window is a fact about the model that
  // endpoints do not report, and headers / extra body are what a relay demands
  // on top of the protocol.
  contextWindow?: number;
  maxOutputTokens?: number;
  headers?: Record<string, string>;
  extraBody?: Record<string, unknown>;
}

// One wire format a source may be saved as, as the kernel declares it. The
// list is the kernel's and the words for it are ours: no label rides along.
export interface Protocol {
  kind: string;
  // What a model on this wire produces. A decision wire returns verdicts to a
  // question set and holds no conversation, so it is offered for the decision
  // role and nowhere else. Absent means chat: it is what every wire was before
  // decision backends, and a kernel that predates the field still answers.
  answers?: "chat" | "decision";
  // Which model-listing shape this wire is discovered under. Protocols sharing
  // one value answer the same listing and cannot be told apart by a probe.
  discovery: string;
  // Whether the wire has a format for a provider-executed web search tool, and
  // whether thinking/effort fields ride it at all.
  serverWebSearch: boolean;
  reasoningParams: boolean;
}

// What an endpoint turned out to be. Every field is a guess the user confirms
// before anything is written — a model list cannot prove which protocol a
// gateway speaks, only which ones it answers.
export interface ProviderProbe {
  kind: string;
  // Every kind that listing may be driven with, kernel order. `kind` is the
  // pre-selection among them, not the only answer: DeepSeek serves both the
  // OpenAI chat wire and the Responses API off one model list.
  kinds: string[];
  authHeader: boolean;
  models: string[];
  default: string;
  efforts: string[];
  effort: string;
  vision: string[];
  // ambiguous: more than one protocol answered, so the kind is a preference
  // rather than a finding.
  ambiguous: boolean;
  // noProxy: it answered only with the proxy bypassed (a China-only endpoint
  // behind a foreign exit resets the handshake).
  noProxy: boolean;
}

// What re-probing a saved provider found. `error` carries the endpoint's own
// words, because "401" and "no chat models" send the user to different fixes.
export interface ProviderCheck {
  ok: boolean;
  kind?: string;
  // Whether that answer is consistent with the kind the entry records. A
  // Responses source answers the OpenAI listing, so equality is the wrong test.
  matches?: boolean;
  models?: string[];
  // Re-probing is also capability discovery: a vendor may add a multimodal
  // model to an endpoint whose stored model list predates it.
  vision?: string[];
  ambiguous?: boolean;
  noProxy?: boolean;
  error?: string;
}

export type ProviderModelCheckStatus = "available" | "unavailable" | "unknown";
export type ProviderModelCheckReason =
  | "auth"
  | "not_found"
  | "rate_limited"
  | "rejected"
  | "network"
  | "timeout"
  // The endpoint answered chat and refused a tools array. Established by
  // sending the same request again without one, not by reading the refusal.
  | "tools";

// A deliberate, billable check of one exact model id. Listing and calling are
// separate facts: private models often accept requests without appearing in a catalog.
export interface ProviderModelCheck {
  model: string;
  status: ProviderModelCheckStatus;
  reason?: ProviderModelCheckReason;
  httpStatus?: number;
}

export interface ProviderModelCheckRequest {
  http1Only?: boolean;
  name?: string;
  model: string;
  baseUrl?: string;
  apiKey?: string;
  kind?: string;
  authHeader?: boolean;
  noProxy?: boolean;
}

// Changing a source that already exists: everything else on the entry stays.
export interface ProviderEdit {
  http1Only?: boolean;
  name: string;
  baseUrl?: string;
  // Empty keeps the stored key.
  apiKey?: string;
  models: string[];
  default: string;
  vision: string[];
  // Omitted means "leave it alone" — sending an empty object instead would
  // clear headers a gateway needs. 0 is a real window: it turns automatic
  // compaction off for this source.
  contextWindow?: number;
  maxOutputTokens?: number;
  headers?: Record<string, string>;
  extraBody?: Record<string, unknown>;
  // Which request shape controls thinking here. "" is auto — no declaration,
  // which leaves the registry in charge. Omitted still means "leave it alone".
  reasoningProtocol?: string;
  // An empty list clears the declared vocabulary; omitted leaves it alone.
  supportedEfforts?: string[];
  defaultEffort?: string;
  // The whole per-model answer for the listed models: a model left out
  // inherits the connection's levels again. Omitted leaves every model alone.
  modelEfforts?: Record<string, ModelEffort>;
}

// One model's effort vocabulary and the level auto resolves to; no default is
// the first level.
export interface ModelEffort {
  supportedEfforts: string[];
  defaultEffort?: string;
}

// What the panel sends back after the user has looked at the probe.
export interface ProviderDraft {
  http1Only?: boolean;
  name: string;
  kind: string;
  baseUrl: string;
  apiKey: string;
  models: string[];
  default: string;
  authHeader: boolean;
  noProxy: boolean;
  effort: string;
  vision: string[];
  contextWindow?: number;
  maxOutputTokens?: number;
  // Optional endpoint compatibility. Omitted values keep the kernel's
  // protocol defaults; they live behind the advanced disclosure in the UI.
  reasoningProtocol?: string;
  headers?: Record<string, string>;
  extraBody?: Record<string, unknown>;
  // Saving under a name already configured is refused unless this is set.
  replace?: boolean;
}

// What the opening sequence still owes a machine with no usable key. GET
// /provider-setup 404s once one exists, so null means "ready".
export interface ProviderSetup {
  required: boolean;
  provider?: string;
  model?: string;
  modelRef?: string;
  keyEnv?: string;
  error?: string;
  activationPending?: boolean;
}

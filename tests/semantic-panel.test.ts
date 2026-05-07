import { beforeAll, describe, expect, it } from "vitest";

(globalThis as { document?: unknown }).document = {
  querySelector: () => ({ getAttribute: () => null }),
};
(globalThis as { localStorage?: Storage }).localStorage = {
  getItem: () => null,
  setItem: () => undefined,
  removeItem: () => undefined,
  clear: () => undefined,
  key: () => null,
  length: 0,
};

type SemanticPanelModule = typeof import("../dashboard/src/panels/semantic.js");

let validateSemanticDraft: SemanticPanelModule["validateSemanticDraft"];

beforeAll(async () => {
  ({ validateSemanticDraft } = await import("../dashboard/src/panels/semantic.js"));
});

describe("validateSemanticDraft", () => {
  it("accepts empty extra body for ollama", () => {
    const result = validateSemanticDraft({
      provider: "ollama",
      ollama: { baseUrl: "http://localhost:11434", model: "nomic-embed-text" },
      openaiCompat: {
        baseUrl: "https://api.example.com/v1/embeddings",
        apiKey: "",
        model: "bge-m3",
        extraBodyText: "",
        apiKeySet: false,
      },
    });
    expect(result.error).toBeNull();
    expect(result.extraBody).toEqual({});
  });

  it("accepts a JSON object for openai-compatible extra body", () => {
    const result = validateSemanticDraft({
      provider: "openai-compat",
      ollama: { baseUrl: "http://localhost:11434", model: "nomic-embed-text" },
      openaiCompat: {
        baseUrl: "https://api.example.com/v1/embeddings",
        apiKey: "sk-test1234567890abcd",
        model: "bge-m3",
        extraBodyText: '{"encoding_format":"float"}',
        apiKeySet: true,
      },
    });
    expect(result.error).toBeNull();
    expect(result.extraBody).toEqual({ encoding_format: "float" });
  });

  it("rejects malformed JSON for openai-compatible extra body", () => {
    const result = validateSemanticDraft({
      provider: "openai-compat",
      ollama: { baseUrl: "http://localhost:11434", model: "nomic-embed-text" },
      openaiCompat: {
        baseUrl: "https://api.example.com/v1/embeddings",
        apiKey: "sk-test1234567890abcd",
        model: "bge-m3",
        extraBodyText: '{\nencoding_format="float"\n}',
        apiKeySet: true,
      },
    });
    expect(result.extraBody).toEqual({});
    expect(result.error).toMatch(/valid JSON/i);
  });

  it("rejects non-object JSON for openai-compatible extra body", () => {
    const result = validateSemanticDraft({
      provider: "openai-compat",
      ollama: { baseUrl: "http://localhost:11434", model: "nomic-embed-text" },
      openaiCompat: {
        baseUrl: "https://api.example.com/v1/embeddings",
        apiKey: "sk-test1234567890abcd",
        model: "bge-m3",
        extraBodyText: "[]",
        apiKeySet: true,
      },
    });
    expect(result.extraBody).toEqual({});
    expect(result.error).toMatch(/JSON object/i);
  });
});

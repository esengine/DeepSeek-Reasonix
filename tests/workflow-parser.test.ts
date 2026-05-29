import { describe, expect, it } from "vitest";
import { parseWorkflowScript } from "../src/workflow/parser.js";

describe("parseWorkflowScript", () => {
  it("accepts literal metadata as the first statement", () => {
    const parsed = parseWorkflowScript(`
export const meta = {
  name: "demo_workflow",
  description: "Demo workflow",
  whenToUse: "When testing workflow parsing",
  phases: [{ title: "Scan", detail: "Collect inputs", model: "deepseek-v4-flash" }],
}

phase("Scan")
return { ok: true }
`);

    expect(parsed.meta.name).toBe("demo_workflow");
    expect(parsed.meta.description).toBe("Demo workflow");
    expect(parsed.meta.phases).toEqual([
      { title: "Scan", detail: "Collect inputs", model: "deepseek-v4-flash" },
    ]);
    expect(parsed.body).toContain('phase("Scan")');
    expect(parsed.body).not.toContain("export const meta");
  });

  it("accepts static template literals in metadata", () => {
    const parsed = parseWorkflowScript(
      "export const meta = { name: `demo`, description: `static` }\nreturn true",
    );

    expect(parsed.meta.name).toBe("demo");
    expect(parsed.meta.description).toBe("static");
  });

  it("requires metadata to be the first statement", () => {
    expect(() =>
      parseWorkflowScript(
        'const x = 1;\nexport const meta = { name: "demo", description: "desc" }',
      ),
    ).toThrow(/first statement/);
  });

  it("requires metadata name and description", () => {
    expect(() => parseWorkflowScript('export const meta = { name: "demo" }')).toThrow(
      /meta\.description/,
    );
    expect(() => parseWorkflowScript('export const meta = { description: "desc" }')).toThrow(
      /meta\.name/,
    );
  });

  it("rejects non-literal metadata", () => {
    expect(() =>
      parseWorkflowScript('export const meta = { name: makeName(), description: "desc" }'),
    ).toThrow(/non-literal node type.*CallExpression/);
    expect(() => parseWorkflowScript('export const meta = { name, description: "desc" }')).toThrow(
      /non-literal node type.*Identifier/,
    );
  });

  it("rejects object metadata hazards", () => {
    expect(() =>
      parseWorkflowScript('export const meta = { ...base, name: "demo", description: "desc" }'),
    ).toThrow(/spread not allowed/);
    expect(() =>
      parseWorkflowScript('export const meta = { ["name"]: "demo", description: "desc" }'),
    ).toThrow(/computed keys not allowed/);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { __proto__: {}, name: "demo", description: "desc" }',
      ),
    ).toThrow(/reserved key name/);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { get name() { return "demo" }, description: "desc" }',
      ),
    ).toThrow(/methods\/accessors not allowed/);
  });

  it("rejects array metadata hazards", () => {
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc", phases: [,,] }',
      ),
    ).toThrow(/sparse arrays not allowed/);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc", phases: [...items] }',
      ),
    ).toThrow(/spread not allowed/);
  });

  it("rejects template interpolation in metadata", () => {
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: `demo_${id}`, description: "desc" }\nreturn true',
      ),
    ).toThrow(/template interpolation not allowed/);
  });

  it("rejects unsafe script APIs", () => {
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc" }\nreturn Date.now()',
      ),
    ).toThrow(/unsafe|deterministic/i);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc" }\nreturn Math.random()',
      ),
    ).toThrow(/unsafe|deterministic/i);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc" }\nreturn new Date()',
      ),
    ).toThrow(/unsafe|deterministic/i);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc" }\nreturn require("fs")',
      ),
    ).toThrow(/unsafe|require/i);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc" }\nreturn import("fs")',
      ),
    ).toThrow(/unsafe|import/i);
    expect(() =>
      parseWorkflowScript(
        'export const meta = { name: "demo", description: "desc" }\nreturn process.env.HOME',
      ),
    ).toThrow(/unsafe|process\.env/i);
  });
});

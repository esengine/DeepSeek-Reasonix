import { readFileSync } from "node:fs";
import ts from "typescript";
import { describe, expect, it } from "vitest";

const MOJIBAKE = /闂\?|濠电姷|鏁告|鈥|�/u;

function userVisibleLiteralIssues(file: string): string[] {
  const source = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const issues: string[] = [];

  function check(node: ts.Node, text: string): void {
    if (!MOJIBAKE.test(text)) return;
    const pos = sf.getLineAndCharacterOfPosition(node.getStart(sf));
    issues.push(`${file}:${pos.line + 1}:${pos.character + 1} ${JSON.stringify(text)}`);
  }

  function visit(node: ts.Node): void {
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      check(node, node.text);
    } else if (
      node.kind === ts.SyntaxKind.TemplateHead ||
      node.kind === ts.SyntaxKind.TemplateMiddle ||
      node.kind === ts.SyntaxKind.TemplateTail
    ) {
      check(node, (node as ts.TemplateLiteralLikeNode).text);
    } else if (ts.isJsxText(node)) {
      check(node, node.getText(sf));
    }
    ts.forEachChild(node, visit);
  }

  visit(sf);
  return issues;
}

describe("App user-visible strings", () => {
  it("does not contain mojibake in runtime string literals", () => {
    expect(userVisibleLiteralIssues("src/cli/ui/App.tsx")).toEqual([]);
  });
});

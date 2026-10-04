// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { boot, STORAGE, t } from "./index";
import { NOTICE_TEXT } from "./notices";
import { say } from "./kernel";
import { workspaceLeaseDetail } from "./workspace_lease";

afterEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });

describe("write claim attribution", () => {
  for (const language of ["zh", "en"]) {
    it(`carries holder identity and extent in ${language}`, () => {
      localStorage.setItem(STORAGE, language);
      boot();
      const claim = { contended: 0, heldMs: 0, idleMs: 0, holder: "Fixture A", holderSessionId: "session-a", paths: ["src/a.go"], requestedPaths: ["src/b.go"] };
      const detail = workspaceLeaseDetail(claim);
      for (const value of ["Fixture A", "session-a", "src/a.go", "src/b.go"]) expect(detail).toContain(value);
      expect(workspaceLeaseDetail(claim, false)).not.toContain("Fixture A");
      expect(t(NOTICE_TEXT.workspace_lease_resumed)).toBe(language === "en"
        ? "This session has acquired its requested write claim and continued"
        : "这个会话已取得所需的写入范围，现已继续");
      expect(say({ code: "workspace.write_conflict" }, "")).not.toBe("");
    });
  }
});

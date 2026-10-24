// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { boot, STORAGE, t } from "./index";
import { NOTICE_TEXT } from "./notices";
import { say } from "./kernel";
import { workspaceLeaseDetail, workspaceLeaseTitle } from "./workspace_lease";

afterEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });

describe("write claim attribution", () => {
  for (const language of ["zh", "en"]) {
    it(`distinguishes execution capacity from file ownership in ${language}`, () => {
      localStorage.setItem(STORAGE, language);
      boot();
      const claim = { contended: 0, heldMs: 0, idleMs: 0, resourceKind: "capacity", state: "waiting", transactionId: "txn-model", holderSessionId: "owner" };
      const detail = workspaceLeaseDetail(claim);
      expect(detail).toContain(language === "en" ? "Execution capacity request" : "执行额度申请");
      expect(detail).not.toContain(language === "en" ? "write claim" : "写入范围");
      for (const state of ["waiting", "acquired", "abandoned"]) {
        expect(workspaceLeaseTitle({ ...claim, state })).toContain(language === "en" ? "capacity" : "执行额度");
      }
      expect(workspaceLeaseTitle({ ...claim, state: "future" })).toBeUndefined();
      expect(workspaceLeaseTitle({ ...claim, resourceKind: "named_paths" })).toBeUndefined();
    });
  }
  for (const language of ["zh", "en"]) {
    it(`renders transaction identity and typed completion reason in ${language}`, () => {
      localStorage.setItem(STORAGE, language);
      boot();
      const claim = { contended: 0, heldMs: 0, idleMs: 0, transactionId: "txn-a", executionId: "exec-b", callId: "call-c", reason: "conflict", waitedMs: 82 };
      const detail = workspaceLeaseDetail(claim, false);
      for (const id of ["txn-a", "exec-b", "call-c", "82"]) expect(detail).toContain(id);
      expect(detail).toContain(language === "en" ? "Input changed" : "输入版本已改变");
      expect(workspaceLeaseDetail({ ...claim, reason: "future_reason" })).toContain("future_reason");
    });
  }
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

  for (const language of ["zh", "en"]) {
    it(`says a repository folded the folders into one write domain, with the way out, in ${language}`, () => {
      localStorage.setItem(STORAGE, language);
      boot();
      const folded = { contended: 0, heldMs: 0, idleMs: 0, holder: "A", holderSessionId: "s", paths: ["d:/home/ws"], requestedPaths: ["d:/home/ws"], repository: "d:/home/ws" };
      const detail = workspaceLeaseDetail(folded);
      expect(detail).toContain("d:/home/ws");
      expect(detail).toContain(language === "en" ? "same Git repository" : "同一个 Git 仓库");
      expect(detail).toContain("write_lease");
      expect(workspaceLeaseDetail({ ...folded, repository: undefined })).not.toContain("write_lease");
      expect(workspaceLeaseDetail(folded, false)).not.toContain("write_lease");
    });
  }
});

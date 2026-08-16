package runtimepolicy

import (
	"testing"

	"reasonix/internal/evidence"
)

func TestParseConstraintsScopesMutationBans(t *testing.T) {
	tests := []struct {
		name        string
		instruction string
		forbid      bool
	}{
		{name: "scoped PR", instruction: "Do not modify PR #10. Create a branch and implement the repair."},
		{name: "scoped product", instruction: "Do not modify Strategy Lab. Commit the Battleboard fix."},
		{name: "scoped list", instruction: "Do not modify:\n- main\n- PR #10\nCreate feature/fix and commit."},
		{name: "unrelated issues", instruction: "Implement the bounded repair. Do not fix unrelated old issues."},
		{name: "descriptive no changes", instruction: "No changes to Battleboard mission prompts — fix the routing layer instead."},
		{name: "read only child", instruction: "Implementation writes are allowed; the independent reviewer is a strict read-only child."},
		{name: "scoped config", instruction: "Write AUDIT.md with the result. Do not change any config."},
		{name: "mixed analysis and fix", instruction: "Analyze the failure, then fix the implementation."},
		{name: "read one file then fix", instruction: "Read only the first file, then fix the implementation."},
		{name: "global analyze only", instruction: "Analyze only the payment flow.", forbid: true},
		{name: "global trailing analyze only", instruction: "Audit the payment flow, analyze only.", forbid: true},
		{name: "global read only review", instruction: "Read-only review of PR #10.", forbid: true},
		{name: "global read only repository", instruction: "Read only the repository.", forbid: true},
		{name: "bare do not modify", instruction: "Do not modify.", forbid: true},
		{name: "broad do not modify", instruction: "Do not modify anything.", forbid: true},
		{name: "without modifying", instruction: "Review the repository without modifying anything.", forbid: true},
		{name: "without changes", instruction: "Inspect the issue without changes.", forbid: true},
		{name: "do not edit files", instruction: "Do not edit any files.", forbid: true},
		{name: "workspace ban", instruction: "Do not change the workspace.", forbid: true},
		{name: "bare no changes", instruction: "No changes.", forbid: true},
		{name: "broad no changes", instruction: "Make no changes to anything.", forbid: true},
		{name: "reproduce only", instruction: "Reproduce only the crash.", forbid: true},
		{name: "scoped Chinese", instruction: "不要修改配置文件，生成 AUDIT.md。"},
		{name: "scoped Chinese issue", instruction: "不要修复无关问题，只处理当前缺陷并提交。"},
		{name: "global Chinese analyze", instruction: "只分析支付流程。", forbid: true},
		{name: "global Chinese read only", instruction: "只读检查当前仓库。", forbid: true},
		{name: "bare Chinese mutation ban", instruction: "不要修改。", forbid: true},
		{name: "global Chinese workspace", instruction: "不要修改当前工作区。", forbid: true},
		{name: "global Chinese reproduce", instruction: "只复现崩溃。", forbid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseConstraints(StripQuotedConstraints(tt.instruction))
			if got.ForbidMutation != tt.forbid {
				t.Fatalf("ForbidMutation = %v, want %v; constraints=%+v", got.ForbidMutation, tt.forbid, got)
			}
			if got.AllowsMutation() == tt.forbid {
				t.Fatalf("AllowsMutation = %v, want %v", got.AllowsMutation(), !tt.forbid)
			}
			decision := (ConstraintGuard{Constraints: got}).BeforeTool(CallContext{
				Profile: evidence.EffectProfile{Known: true, WorkspaceWrite: true},
			})
			if (decision.Action == GuardDeny) != tt.forbid {
				t.Fatalf("writer decision = %+v, forbid=%v", decision, tt.forbid)
			}
		})
	}
}

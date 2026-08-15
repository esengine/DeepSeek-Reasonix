package agent

import (
	"context"
	"encoding/json"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// toolCallPlan is the resolved, policy-checked state owned by one executeOne.
type toolCallPlan struct {
	call                                          provider.ToolCall
	tool                                          tool.Tool
	canonicalName                                 string
	permName                                      string
	permArgs                                      json.RawMessage
	execTool                                      tool.Tool
	execArgs                                      json.RawMessage
	evidenceName                                  string
	evidenceArgs                                  json.RawMessage
	readOnly                                      bool
	resolved                                      tool.ResolvedCall
	resolvedMeta                                  *tool.ResolvedCall
	effects                                       evidence.ToolEffects
	verification, planTransition                  bool
	planBefore, planAfter, planDiff               string
	planReplacementAuthorized                     bool
	recoveryGen                                   uint64
	runTool                                       tool.Tool
	runArgs                                       json.RawMessage
	cctx                                          context.Context
	releaseParentWrite, releaseMutationWrite      func()
	mutationPath                                  string
	mutationObserved, mutationAfterDone, executed bool
	perCallWriteRoots                             []string
	skipOrdinaryGate                              bool
}

func (p *toolCallPlan) classifyEffects() {
	p.effects = evidence.ClassifyToolCall(p.evidenceName, p.evidenceArgs, p.readOnly)
}

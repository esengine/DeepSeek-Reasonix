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
	call          provider.ToolCall
	tool          tool.Tool
	canonicalName string
	permName      string
	permArgs      json.RawMessage
	execTool      tool.Tool
	execArgs      json.RawMessage
	evidenceName  string
	evidenceArgs  json.RawMessage
	readOnly      bool
	resolved      tool.ResolvedCall
	resolvedMeta  *tool.ResolvedCall
	effects       evidence.ToolEffects
	profile       evidence.EffectProfile
	verification  bool
	runTool       tool.Tool
	runArgs       json.RawMessage
	// readTaskID is the logical read a continuation call joined, empty for a
	// fresh read.
	readTaskID          string
	readEnvelope        *tool.ReadResultEnvelope
	readActiveMillis    int64
	readSnapshot        string
	expectedWriteSource tool.EvidenceTargetInfo
	cctx                context.Context
	// mcpApp collects the call's Apps presentation from the executing tool.
	mcpApp                                                 *tool.MCPAppResult
	presentedFiles                                         func() []tool.PresentedFile
	releaseParentWrite, releaseMutationWrite, releaseLease func()
	parentWriteClaimID                                     int64
	mutationPath                                           string
	mutationObserved, mutationAfterDone, executed          bool
	hooksMayMutateWorkspace                                bool
	perCallWriteRoots                                      []string
	skipOrdinaryGate                                       bool
	permissionPreset                                       string
}

func cloneEvidenceTarget(target tool.EvidenceTargetInfo) tool.EvidenceTargetInfo {
	target.Ranges = append([]tool.ReadRange(nil), target.Ranges...)
	target.Hashes = append([]string(nil), target.Hashes...)
	return target
}

func (p *toolCallPlan) classifyEffects() {
	p.effects = evidence.ClassifyToolCall(p.evidenceName, p.evidenceArgs, p.readOnly)
}

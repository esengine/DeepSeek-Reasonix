package isolation

import (
	"encoding/json"
	"reflect"
	"slices"

	"reasonix/internal/safety/evidence"
)

// CandidateEvidence records observations in one environment. It is never
// source-workspace evidence, nor proof that the finished candidate was tested.
type CandidateEvidence struct {
	EnvironmentID   string                    `json:"environment_id"`
	Workspace       string                    `json:"workspace"`
	Verification    string                    `json:"verification"`
	Receipts        []CandidateReceipt        `json:"receipts"`
	FinishedContent *evidence.ContentSnapshot `json:"finished_content"`
	Checks          []CheckEvidence           `json:"checks"`
}

// CheckEvidence is observed by the host, independently of the model ledger.
// Success describes the command result; Inputs retain any coverage gaps.
type CheckEvidence struct {
	ExecutionID   string                 `json:"execution_id"`
	Contract      string                 `json:"contract"`
	CommandDigest string                 `json:"command_digest"`
	Success       bool                   `json:"success"`
	Inputs        []evidence.CheckInputs `json:"inputs"`
}

func mergeEvidenceFields(previous, next json.RawMessage, before, after *CandidateEvidence) (json.RawMessage, error) {
	if before == nil || after == nil {
		return next, nil
	}
	merged, err := mergeIdentityFields(previous, next)
	if err != nil {
		return nil, err
	}
	var oldFields, newFields map[string]json.RawMessage
	if err := json.Unmarshal(previous, &oldFields); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(merged, &newFields); err != nil {
		return nil, err
	}
	var oldReceipts, newReceipts []json.RawMessage
	if raw := oldFields["receipts"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &oldReceipts); err != nil {
			return nil, err
		}
	}
	if raw := newFields["receipts"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &newReceipts); err != nil {
			return nil, err
		}
	}
	for i := range min(len(before.Receipts), len(after.Receipts), len(oldReceipts), len(newReceipts)) {
		if !reflect.DeepEqual(before.Receipts[i], after.Receipts[i]) {
			break
		}
		newReceipts[i] = oldReceipts[i]
	}
	newFields["receipts"], err = json.Marshal(newReceipts)
	if err != nil {
		return nil, err
	}
	if reflect.DeepEqual(before.Checks, after.Checks) {
		if prior, exists := oldFields["checks"]; exists {
			newFields["checks"] = prior
		}
	}
	return json.Marshal(newFields)
}

// Tool arguments and command text can contain credentials or file contents;
// retain host observations rather than serializing the live ledger wholesale.
type CandidateReceipt struct {
	Tool         string   `json:"tool"`
	ExecutionID  string   `json:"execution_id,omitempty"`
	Success      bool     `json:"success"`
	Mutation     bool     `json:"mutation,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	ExitCode     *int     `json:"exit_code,omitempty"`
	Verification string   `json:"verification,omitempty"`
	OutputDigest string   `json:"output_digest,omitempty"`
}

func CaptureCandidateEvidence(entry *Entry, summary evidence.ChildEvidenceSummary) *CandidateEvidence {
	result := &CandidateEvidence{EnvironmentID: entry.ID, Workspace: entry.Cand.WorkspaceRoot, Verification: "unknown"}
	for _, r := range summary.Receipts {
		var exit *int
		if r.ExitCode != nil {
			v := *r.ExitCode
			exit = &v
		}
		result.Receipts = append(result.Receipts, CandidateReceipt{Tool: r.ToolName, ExecutionID: r.SourceExecutionID, Success: r.Success, Mutation: r.Mutation, Paths: slices.Clone(r.Paths), ExitCode: exit, Verification: r.Verification, OutputDigest: r.OutputDigest})
	}
	return result
}

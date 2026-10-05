package config

import "strings"

// ModelMode is an optional way a model can be asked to run, declared beside
// its effort ladder. It is a request parameter: it never reaches the prompt.
type ModelMode struct {
	ID string
	// LabelKey and HintKey name the frontend's strings for the switch.
	LabelKey string
	HintKey  string
	// ReasoningMode is the Responses reasoning.mode value the mode sends.
	ReasoningMode string
	// Costlier marks a mode the vendor documents as spending more per turn.
	Costlier bool
}

// modelModeDecl binds a mode to the wires and the endpoint it was established
// on. Both are its own: a model's ladder may reach a wire its modes do not.
type modelModeDecl struct {
	Mode   ModelMode
	Kinds  []string
	Vendor vendorEndpoint
}

// openAIProMode is developers.openai.com/api/docs/guides/reasoning: GPT-5.6
// and GPT-6 take reasoning.mode "pro" on the Responses API only, independent
// of reasoning.effort, billed at the model's rates for more tokens.
var openAIProMode = modelModeDecl{
	Mode: ModelMode{
		ID: "pro", LabelKey: "model_mode.pro", HintKey: "model_mode.pro.hint",
		ReasoningMode: "pro", Costlier: true,
	},
	Kinds:  []string{kindResponses},
	Vendor: openAIVendor,
}

// RequestModes is the modes a request for this entry may carry: the model
// table's declarations whose wire and endpoint match the entry in hand. A relay
// serving the same model id gets none — nobody has shown it forwards them.
func RequestModes(e *ProviderEntry) []ModelMode {
	if e == nil {
		return nil
	}
	cap, ok := modelReasoningCapabilities[strings.ToLower(strings.TrimSpace(e.Model))]
	if !ok {
		return nil
	}
	var out []ModelMode
	for _, decl := range cap.Modes {
		if containsString(decl.Kinds, e.Kind) && servedByVendor(e, decl.Vendor) {
			out = append(out, decl.Mode)
		}
	}
	return out
}

// RequestReasoningModes projects RequestModes onto the Responses wire: mode id
// to the reasoning.mode value it sends. A provider sends nothing outside it.
func RequestReasoningModes(e *ProviderEntry) map[string]string {
	modes := RequestModes(e)
	if len(modes) == 0 {
		return nil
	}
	out := make(map[string]string, len(modes))
	for _, mode := range modes {
		if mode.ReasoningMode != "" {
			out[mode.ID] = mode.ReasoningMode
		}
	}
	return out
}

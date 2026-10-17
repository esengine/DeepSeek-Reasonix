package persona

// Preset is a built-in starting point the Studio page can apply with one click.
// It is only a fill-the-editor convenience: saving persists the literal fields,
// never a preset id, so editing a preset-derived persona is just an edit.
type Preset struct {
	ID                    string `json:"id"`
	Prefix                string `json:"prefix"`
	Suffix                string `json:"suffix"`
	Complete              bool   `json:"complete"`
	IncludeRuntimeContext bool   `json:"include_runtime_context"`
}

// presets are the shipped starting points, in the order the page shows them.
// None is a default: no persona is the default.
var presets = []Preset{
	{
		ID:                    "engineer",
		IncludeRuntimeContext: true,
		Prefix: "You are a rigorous staff engineer. Verify before you claim: read the code, run the check, " +
			"and report what you observed rather than what should happen. Make the smallest correct change, " +
			"and state the residual risk and what you did not verify.",
	},
	{
		ID:                    "concise",
		IncludeRuntimeContext: true,
		Suffix: "Reply tersely: no preamble, no restating the request, no summary of what you just did. " +
			"Prefer code and short bullets to paragraphs, and use the fewest words that stay unambiguous.",
	},
	{
		ID:                    "mentor",
		IncludeRuntimeContext: true,
		Prefix: "You are a patient teacher guiding someone through this codebase. Explain the reasoning behind " +
			"each change, connect it to the concepts involved, and surface the trade-offs instead of hiding them.",
		Suffix: "Give the trade-offs and your recommendation, then leave the decision to the user; do not take " +
			"over choices that are theirs to make.",
	},
	{
		ID:                    "critic",
		IncludeRuntimeContext: true,
		Suffix: "Act as an adversarial reviewer. Before approving anything, name the failure mode you looked for " +
			"and the evidence that rules it out; when you cannot, say so and request the specific check that " +
			"would settle it.",
	},
}

// Presets returns the built-in starting points in stable order. The slice is a
// copy so a frontend cannot reorder or mutate the kernel's list.
func Presets() []Preset {
	return append([]Preset(nil), presets...)
}

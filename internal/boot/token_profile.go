package boot

// Retired economy/delivery mode-specific system prompts. Kept as negative
// fixtures so boot tests can prove role settings never inject them again.
// Provider-visible tools are unified across light|balanced|delivery; optional
// tools dispatch through use_capability without connect_tool_source.

const tokenEconomyPrompt = `Economy mode is on. Keep work direct and use connect_tool_source only when the task needs a capability absent from the core file and shell tools.`

const tokenDeliveryPrompt = `<delivery-profile>
Prioritize a verified, complete result over minimizing model calls or tokens.
For action requests: establish acceptance criteria; reproduce bugs when practical;
inspect the relevant code and project rules; fix the root cause; run focused
verification; review the resulting diff and adjacent behavior; and continue until
the request is complete or a genuine blocker remains. Do not claim success without
evidence. State any unverified result or assumption explicitly.
</delivery-profile>`

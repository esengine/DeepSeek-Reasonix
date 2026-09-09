# Desktop prompt identity

Desktop decision cards are owned by the controller that created them. Every
new prompt request carries a prompt id, the owning turn id, and the runtime
epoch visible to the tab. The kind identifies the decision surface: `ask`,
`approval`, `plan`, `recovery`, or `mcp`.

The frontend submits these values through `ResolvePromptForTab`. The controller
checks the runtime epoch, active turn, prompt owner, and pending state under its
exact-resolution boundary before persisting `PromptAnswered` and waking the
original waiter. A stale turn or runtime is rejected without routing the answer
to a replacement controller. Failed persistence restores the prompt to its
pending state so the user can retry.

Prompt requests and lifecycle events expose `promptId`, `promptKind`, and
`turnId`. Desktop event envelopes carry the tab runtime epoch. Events without a
turn identity are marked `promptLegacy` and are accepted only by compatibility
paths.

Older Wails methods such as `AnswerQuestionForTab`, `ApproveTab`, and
`ResolveRecoveryTab` remain available for older clients. New frontend code uses
`ResolvePromptForTab` and does not silently downgrade to an unfenced method.
When a stale response is received, the card is removed from the active decision
surface and one tab-scoped prompt replay is requested; only a new pending
identity can re-arm a card.

// The one thing in the fixture tree that reaches a mutating verb. Every case
// below states whether a state write can retrigger an effect that reaches it.
export const mutation = () => void fetch("/fixture/write", { method: "POST" });

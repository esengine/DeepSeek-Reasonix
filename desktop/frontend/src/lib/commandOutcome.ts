export type CommandCancellationReason = "not-ready" | "superseded" | "disposed";
export type CommandOutcome<T> =
  | { status: "completed"; value: T }
  | { status: "cancelled"; reason: CommandCancellationReason }
  | { status: "failed"; error: unknown };

export class CommandCancelled extends Error {
  constructor(readonly reason: CommandCancellationReason) {
    super(reason);
  }
}

export type CommandAuthority = {
  checkpoint(): void;
};

/** Standalone execution holds captured input, never the capture callback or DOM event. */
export async function executeCapturedCommand<Input, Result, Authority extends CommandAuthority>(
  input: Input,
  execute: (input: Input, authority: Authority) => Result,
  authority: Authority,
): Promise<CommandOutcome<Awaited<Result>>> {
  try {
    authority.checkpoint();
    const value = await execute(input, authority);
    authority.checkpoint();
    return { status: "completed", value: value as Awaited<Result> };
  } catch (error) {
    try { authority.checkpoint(); } catch (cancelled) { error = cancelled; }
    return error instanceof CommandCancelled
      ? { status: "cancelled", reason: error.reason }
      : { status: "failed", error };
  }
}

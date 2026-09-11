import { useCommittedCommand } from "./useCommittedCommand";

/** Stable event authority retaining only the latest committed presentation. */
export function useTranscriptCommand<Args extends unknown[], Result>(command: (...args: Args) => Result) {
  // Transcript callbacks are invoked only while their surface is mounted, so
  // retain the existing exact result type at this compatibility boundary.
  return useCommittedCommand(command) as (...args: Args) => Result;
}

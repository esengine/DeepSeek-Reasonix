import { createContext, useContext } from "react";

// The agent whose own conversation a pane shows; empty off the team and in a
// group, where no one agent owns the conversation.
export const SeatAgent = createContext("");

export function useSeatAgent(): string {
  return useContext(SeatAgent);
}

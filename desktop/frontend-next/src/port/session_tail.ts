export interface SessionTailPort {
  // Copies the newest part of a session too large to open into a new session;
  // dropped is how many earlier messages were left out. The source is untouched.
  continueFromTail(path: string): Promise<{ path: string; dropped: number }>;
}

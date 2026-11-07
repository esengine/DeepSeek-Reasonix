// A resume through an expired provider cache: the idle gap, the prompt the next
// request resends uncached, the model's window, and whether the host offers a fold.
export interface ColdResume {
  idle_seconds: number;
  tokens: number;
  window?: number;
  compact?: boolean;
}

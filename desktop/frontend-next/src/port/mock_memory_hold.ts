import type { MemoryHoldPort, MemoryProactive, MemorySuggestion } from "./memory";
import { MockTerminal } from "./mock_terminal";

export class MockMemoryHold extends MockTerminal implements MemoryHoldPort {
  proactive: MemoryProactive = "ask";
  suggestions: MemorySuggestion[] = [];
  async memoryProactive() {
    return this.proactive;
  }
  async setMemoryProactive(mode: MemoryProactive) {
    this.proactive = mode;
  }
  async memorySuggestions() {
    return this.suggestions;
  }
  async acceptMemorySuggestion(id: string) {
    this.suggestions = this.suggestions.filter((s) => s.id !== id);
  }
  async dismissMemorySuggestion(id: string) {
    this.suggestions = this.suggestions.filter((s) => s.id !== id);
  }
}

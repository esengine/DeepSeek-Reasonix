import { describe, expect, it } from "vitest";
import { suggestSlashCommands } from "../src/cli/ui/slash/commands.js";

describe("suggestSlashCommands with favorites", () => {
  it("puts favorited commands first in group mode (empty prefix)", () => {
    const result = suggestSlashCommands("", false, undefined, ["help", "new"]);
    // help and new should be the first two
    expect(result[0]?.cmd).toBe("help");
    expect(result[1]?.cmd).toBe("new");
    // Non-favorites come after
    const rest = result.slice(2);
    for (const c of rest) {
      expect(c.cmd).not.toBe("help");
      expect(c.cmd).not.toBe("new");
    }
  });

  it("puts favorited commands first in search mode", () => {
    const result = suggestSlashCommands("c", false, undefined, ["compact", "copy"]);
    expect(result[0]?.cmd).toBe("compact");
    expect(result[1]?.cmd).toBe("copy");
  });

  it("puts favorited commands first with counts-based sorting", () => {
    const counts: Record<string, number> = { help: 5, new: 3, retry: 10, compact: 1 };
    const result = suggestSlashCommands("", false, counts, ["compact", "retry"]);
    // Favorites first (preserving group order: retry before compact in SLASH_COMMANDS)
    expect(result[0]?.cmd).toBe("retry");
    expect(result[1]?.cmd).toBe("compact");
    // Then non-favorites sorted by count
    const rest = result.slice(2);
    const helpIdx = rest.findIndex((c) => c.cmd === "help");
    const newIdx = rest.findIndex((c) => c.cmd === "new");
    expect(helpIdx).toBeGreaterThanOrEqual(0);
    expect(newIdx).toBeGreaterThanOrEqual(0);
  });

  it("works correctly with no favorites", () => {
    const withFavs = suggestSlashCommands("", false, undefined, []);
    const withoutFavs = suggestSlashCommands("", false, undefined, undefined);
    expect(withFavs.map((c) => c.cmd)).toEqual(withoutFavs.map((c) => c.cmd));
  });

  it("ignores favorite names that don't match any command", () => {
    const result = suggestSlashCommands("", false, undefined, ["nonexistent", "help"]);
    expect(result[0]?.cmd).toBe("help");
  });
});

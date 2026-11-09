import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Sym, iconFor } from "./Sym";

describe("tool icons describe the operation", () => {
  it.each([
    ["read_file", "file-text"], ["grep", "search"], ["ls", "folder"],
    ["kill_shell", "square"], ["wait", "clock"], ["delete_range", "trash-2"],
    ["web_search", "search"], ["web_fetch", "globe"],
  ])("distinguishes %s with a %s library icon", (tool, icon) => {
    const html = renderToStaticMarkup(<Sym icon={iconFor(tool)} done />);
    expect(html).toContain(`lucide-${icon}`);
    expect(html).toContain('aria-hidden="true"');
    expect(html).toContain('data-done=""');
  });

  it.each(["unlisted_tool", "constructor", "toString", "__proto__"])("uses a neutral tool icon for %s", (tool) => {
    const html = renderToStaticMarkup(<Sym icon={iconFor(tool)} />);
    expect(html).toContain("lucide-wrench");
    expect(html).not.toContain("lucide-check");
    expect(renderToStaticMarkup(<Sym icon={iconFor("mcp__custom__action")} />)).toContain("lucide-plug");
  });
});

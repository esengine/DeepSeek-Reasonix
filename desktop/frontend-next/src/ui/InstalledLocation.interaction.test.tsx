// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import { useRef, useState } from "react";
import { InstalledLocation } from "./InstalledLocation";

afterEach(cleanup);

it("clears an absent installed target after refresh so later list updates cannot steal focus", async () => {
  const onFound = vi.fn();
  function Harness({ refreshing, packages }: { refreshing: boolean; packages: unknown[] }) {
    const root = useRef<HTMLDivElement>(null);
    const [target, setTarget] = useState<{ kind: string; name: string } | null>({ kind: "plugin", name: "missing" });
    return <div ref={root}>
      <button data-action="extensions.tab" data-value="installed">Installed</button>
      <div id="set-plugins" />
      <InstalledLocation root={root} target={target} packages={packages} skills={[]} mcp={[]} refreshing={refreshing} onFound={(next) => {
        onFound(next);
        setTarget(next);
      }} />
    </div>;
  }

  const { rerender } = render(<Harness refreshing packages={[]} />);
  expect(onFound).not.toHaveBeenCalled();
  rerender(<Harness refreshing={false} packages={[]} />);
  await waitFor(() => expect(onFound).toHaveBeenCalledExactlyOnceWith(null));
  rerender(<Harness refreshing={false} packages={[{}]} />);
  expect(onFound).toHaveBeenCalledTimes(1);
});

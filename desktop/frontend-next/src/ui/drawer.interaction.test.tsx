// @vitest-environment jsdom
import { useState } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { closeGroupStage, openGroupStage } from "../state/teamStage";
import { useDrawerCloses } from "./drawer";

function Scene({ active = "ordinary" }: { active?: string }) {
  const [rail, setRail] = useState(false);
  useDrawerCloses(setRail, active, false);
  return <><button onClick={() => setRail(true)}>Open drawer</button><output>{rail ? "open" : "closed"}</output></>;
}

afterEach(() => { cleanup(); closeGroupStage(); delete document.documentElement.dataset.fold; });

describe("navigation closes the phone drawer", () => {
  it("closes after group selection and ordinary session selection", () => {
    document.documentElement.dataset.fold = "scene";
    const view = render(<Scene />);
    fireEvent.click(screen.getByText("Open drawer"));
    expect(screen.getByRole("status").textContent).toBe("open");
    act(() => openGroupStage("group-a"));
    expect(screen.getByRole("status").textContent).toBe("closed");
    fireEvent.click(screen.getByText("Open drawer"));
    act(() => openGroupStage("group-a"));
    expect(screen.getByRole("status").textContent).toBe("closed");
    fireEvent.click(screen.getByText("Open drawer"));
    view.rerender(<Scene active="agent-b" />);
    expect(screen.getByRole("status").textContent).toBe("closed");
  });

  it("keeps the desktop sidebar open when selecting a group", () => {
    render(<Scene />);
    fireEvent.click(screen.getByText("Open drawer"));
    act(() => openGroupStage("group-a"));
    expect(screen.getByRole("status").textContent).toBe("open");
  });
});

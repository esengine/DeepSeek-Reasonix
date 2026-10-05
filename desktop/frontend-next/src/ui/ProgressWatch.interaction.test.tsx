// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ProgressWatch } from "./ProgressWatch";
import type { AgentPort } from "../port/port";
import type { ProgressWatchSettings } from "../port/boundary";

afterEach(cleanup);

const base: ProgressWatchSettings = { pause: false, rounds: 20, tokenMultiple: 8, defaultRounds: 20, defaultTokenMultiple: 8, path: "/u/config.toml" };

function portWith(save: (s: Pick<ProgressWatchSettings, "pause" | "rounds" | "tokenMultiple">) => Promise<ProgressWatchSettings>) {
  return { progressWatch: vi.fn(async () => base), saveProgressWatch: vi.fn(save) } as unknown as AgentPort & { saveProgressWatch: ReturnType<typeof vi.fn> };
}

describe("the progress watch setting", () => {
  it("ships with pausing off and writes the switch with the numbers it holds", async () => {
    const port = portWith(async (s) => ({ ...base, ...s }));
    render(<ProgressWatch port={port} />);
    const sw = await screen.findByRole("switch", { name: "长时间无进展时暂停任务" });
    expect(sw.getAttribute("aria-checked")).toBe("false");
    await userEvent.click(sw);
    expect(port.saveProgressWatch).toHaveBeenCalledWith({ pause: true, rounds: 20, tokenMultiple: 8 });
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("true"));
  });

  it("commits a round count once, on Enter", async () => {
    const port = portWith(async (s) => ({ ...base, ...s }));
    render(<ProgressWatch port={port} />);
    const field = await screen.findByRole("textbox", { name: "连续无进展轮数" });
    await userEvent.clear(field);
    await userEvent.type(field, "12{Enter}");
    expect(port.saveProgressWatch).toHaveBeenCalledTimes(1);
    expect(port.saveProgressWatch).toHaveBeenCalledWith({ pause: false, rounds: 12, tokenMultiple: 8 });
  });

  it("refuses a number out of range without writing", async () => {
    const port = portWith(async (s) => ({ ...base, ...s }));
    render(<ProgressWatch port={port} />);
    const field = await screen.findByRole("textbox", { name: "输入 token 上限" });
    await userEvent.clear(field);
    await userEvent.type(field, "0{Enter}");
    expect(port.saveProgressWatch).not.toHaveBeenCalled();
    expect(screen.getByText("请输入 1 到 1000 之间的整数。")).toBeTruthy();
  });
});

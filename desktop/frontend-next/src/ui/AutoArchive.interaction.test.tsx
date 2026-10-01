// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AutoArchive } from "./AutoArchive";
import type { AgentPort } from "../port/port";
import type { AutoArchiveSettings } from "../port/boundary";

afterEach(cleanup);

const base: AutoArchiveSettings = { enabled: false, days: 30, defaultDays: 30, path: "/u/config.toml" };

function portWith() {
  return {
    autoArchive: vi.fn(async () => base),
    saveAutoArchive: vi.fn(async (s: Pick<AutoArchiveSettings, "enabled" | "days">) => ({ ...base, ...s })),
  } as unknown as AgentPort & { saveAutoArchive: ReturnType<typeof vi.fn> };
}

describe("the auto-archive setting", () => {
  it("ships off and writes the switch with the days it holds", async () => {
    const port = portWith();
    render(<AutoArchive port={port} />);
    const sw = await screen.findByRole("switch", { name: "自动归档长时间未活动的对话" });
    expect(sw.getAttribute("aria-checked")).toBe("false");
    await userEvent.click(sw);
    expect(port.saveAutoArchive).toHaveBeenCalledWith({ enabled: true, days: 30 });
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("true"));
  });

  it("commits the days once, on Enter, and refuses zero", async () => {
    const port = portWith();
    render(<AutoArchive port={port} />);
    const field = await screen.findByRole("textbox", { name: "闲置天数" });
    await userEvent.clear(field);
    await userEvent.type(field, "3{Enter}");
    expect(port.saveAutoArchive).toHaveBeenCalledTimes(1);
    expect(port.saveAutoArchive).toHaveBeenCalledWith({ enabled: false, days: 3 });
    await userEvent.clear(field);
    await userEvent.type(field, "0{Enter}");
    expect(port.saveAutoArchive).toHaveBeenCalledTimes(1);
    expect(screen.getByText("请输入 1 到 3650 之间的整数。")).toBeTruthy();
  });
});

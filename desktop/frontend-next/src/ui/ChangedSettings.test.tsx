// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ChangedSettings } from "./ChangedSettings";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const portWith = (keys: string[]) => ({ changedSettings: vi.fn(async () => ({ keys, path: "/u/config.toml" })) }) as unknown as AgentPort;

describe("the changed-settings card", () => {
  it("says how many settings differ from the defaults and lists them on request", async () => {
    render(<ChangedSettings port={portWith(["ui.fold_turn_work", "language"])} />);
    expect(await screen.findByText("有 2 项和默认值不同")).toBeTruthy();
    expect(screen.queryByText("ui.fold_turn_work")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "查看" }));
    expect(screen.getByText("ui.fold_turn_work")).toBeTruthy();
    expect(screen.getByText("language")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "收起" }));
    expect(screen.queryByText("language")).toBeNull();
  });

  it("offers nothing to open when every setting is the default", async () => {
    render(<ChangedSettings port={portWith([])} />);
    expect(await screen.findByText("所有设置都是默认值")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "查看" })).toBeNull();
  });
});

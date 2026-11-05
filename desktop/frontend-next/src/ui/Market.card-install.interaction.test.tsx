// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Market } from "./Market";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const card = (name: string) => screen.getByText(name, { selector: ".nm" }).closest("li") as HTMLElement;

it("installs from the card: a pinned package goes straight to what it would install", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const plan = vi.spyOn(port, "planMarket");
  render(<Market port={port} onInstalled={() => {}} />);
  await screen.findByText("review-kit", { selector: ".nm" });
  await userEvent.click(within(card("review-kit")).getByRole("button", { name: "安装" }));
  await waitFor(() => expect(plan).toHaveBeenCalledWith(expect.objectContaining({ slug: "acme/review-kit" })));
  expect(await screen.findByText(/将安装以下内容/)).toBeTruthy();
});

it("offers no shortcut for a package nobody pinned, or one already installed", async () => {
  render(<Market port={new MockPort() as unknown as AgentPort} onInstalled={() => {}} />);
  await userEvent.click(await screen.findByRole("checkbox", { name: "只看已固定" }));
  await screen.findByText("lm-studio-vision-bridge", { selector: ".nm" });
  expect(within(card("lm-studio-vision-bridge")).queryByRole("button", { name: "安装" })).toBeNull();
  expect(within(card("irmia-devkit")).queryByRole("button", { name: "安装" })).toBeNull();
});

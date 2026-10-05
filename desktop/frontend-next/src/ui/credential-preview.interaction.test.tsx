// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddServer } from "./AddServer";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

it("renders the display endpoint and installs the operational endpoint", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const server = {
    name: "neutral", transport: "http",
    url: "https://host/mcp?token=fixture-secret",
    displayUrl: "https://host/mcp?token=%3Credacted%3E",
    env: { PASSWORD: "fixture-secret" }, displayEnv: { PASSWORD: "<redacted>" },
    headers: { Authorization: "fixture-secret" }, displayHeaders: { Authorization: "<redacted>" },
  };
  const stdio = { name: "neutral-stdio", transport: "stdio", command: "node", args: ["--password", "fixture-secret"], displayCommand: "node", displayArgs: ["--password", "<redacted>"] };
  vi.spyOn(port, "parseMcp").mockResolvedValue({ servers: [server, stdio], risks: [
    { server: "neutral", kind: "unknown-host", field: "url", detail: "obsolete endpoint" },
    { server: "neutral-stdio", kind: "shell", field: "command", detail: "node --password fixture-secret" },
  ] });
  const install = vi.spyOn(port, "installMcp").mockResolvedValue({ name: "neutral", state: "ready", action: "installed", toolCount: 0, message: "" });
  render(<AddServer port={port} canProject onClose={() => {}} onInstalled={() => {}} />);
  await userEvent.type(screen.getByRole("textbox"), "neutral fixture");
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  expect(screen.getByText(server.displayUrl)).toBeTruthy();
  expect(screen.getByText("node --password <redacted>")).toBeTruthy();
  expect(document.body.textContent).not.toContain("fixture-secret");
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  expect(install).toHaveBeenNthCalledWith(1, server, "user");
  expect(install).toHaveBeenNthCalledWith(2, stdio, "user");
});

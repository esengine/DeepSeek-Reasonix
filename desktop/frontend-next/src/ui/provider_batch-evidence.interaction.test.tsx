// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import type { ProviderEntry } from "../port/port";
import type { Port } from "./Providers";
import { EditConn } from "./EditConn";

afterEach(cleanup);

it("replaces a previous HTTP refusal with the current batch's network failure", async () => {
  const entry: ProviderEntry = {
    name: "relay", kind: "openai", baseUrl: "https://relay.example/v1",
    models: ["m1"], default: "m1", hasKey: true, inUse: false, preset: false,
  };
  const checkProviderModel = vi.fn()
    .mockResolvedValueOnce({ model: "m1", status: "unavailable", reason: "not_found", httpStatus: 404, detail: "no such model" })
    .mockRejectedValueOnce(new Error("connection closed"));
  const port = { checkProviderModel } as unknown as Port;
  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  const user = userEvent.setup();
  const all = screen.getByRole("button", { name: /^测试已启用模型/ });
  await user.click(all);
  await screen.findByText("HTTP 404");
  expect(screen.getByText("no such model")).toBeTruthy();
  await user.click(all);
  await waitFor(() => expect(document.querySelector('.mevidence i[data-state="unknown"]')).not.toBeNull());
  expect(checkProviderModel).toHaveBeenCalledTimes(2);
  expect(screen.queryByText("HTTP 404")).toBeNull();
  expect(screen.queryByText("no such model")).toBeNull();
});

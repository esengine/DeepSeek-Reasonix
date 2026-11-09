// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { ModelSettings } from "./ModelSettings";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

it("expands services, uses and context controls in two full cards", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const models = await port.models();
  const onChanged = vi.fn();
  const onFailed = vi.fn();
  render(<ModelSettings port={port} onChanged={onChanged} onFailed={onFailed}
    usage={{ models, roles: null, busy: "", protocol: {}, onMain: vi.fn(), onRole: vi.fn() }}
    providers={{ port, onChanged, onFailed, protocol: {}, onProtocol: vi.fn(), activeKindFor: (account) => account.kinds[0] }} />);
  expect(screen.getByRole("heading", { name: "服务" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "用途与上下文" })).toBeTruthy();
  await waitFor(() => {
    expect(document.querySelector('#set-providers button')).toBeTruthy();
    expect(document.querySelector('#set-model select')).toBeTruthy();
    expect(document.querySelector('#set-context .cmp')).toBeTruthy();
  });
});

// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Account } from "./Account";
import { MockPort } from "../port/mock";
import { HttpError, type AccountState, type AgentPort, type DeviceGrant } from "../port/port";
import { boot, STORAGE } from "../i18n";

const setLocale = (lang: "zh" | "en") => { localStorage.setItem(STORAGE, lang); boot(); };

afterEach(cleanup);

describe("account sign-in", () => {
  it("hands the approval URL to the host browser", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.accountLogin = async () => ({
      deviceCode: "device-secret",
      userCode: "ABCD-EFGH",
      verificationUri: "https://reasonix.io/device/",
      verificationUriComplete: "https://reasonix.io/device/?code=ABCD-EFGH",
      interval: 1,
      expiresIn: 0,
    });
    port.openExternal = vi.fn(async () => {});

    render(<Account port={port} state={{ signedIn: false }} reload={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "登录" }));

    await waitFor(() => expect(port.openExternal).toHaveBeenCalledWith("https://reasonix.io/device/?code=ABCD-EFGH"));
  });
});

describe("account sign-in failures", () => {
  afterEach(() => setLocale("zh"));

  const expiring: DeviceGrant = {
    deviceCode: "device-secret",
    userCode: "ABCD-EFGH",
    verificationUri: "https://reasonix.io/device/",
    verificationUriComplete: "https://reasonix.io/device/?code=ABCD-EFGH",
    interval: 1,
    expiresIn: 0,
  };

  it("words a refused sign-in from the kernel's code", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.accountLogin = async () => {
      throw new HttpError(403, "account sign-in is not enabled for this server", {
        code: "account.signin_disabled",
        error: "account sign-in is not enabled for this server",
      });
    };
    render(<Account port={port} state={{ signedIn: false }} reload={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByText("这台服务器未开放账号登录")).toBeTruthy();
  });

  it("says an expired request in the window's language", async () => {
    setLocale("en");
    const port = new MockPort() as unknown as AgentPort;
    port.accountLogin = async () => expiring;
    port.openExternal = vi.fn(async () => {});
    render(<Account port={port} state={{ signedIn: false }} reload={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("This sign-in request expired. Try again.")).toBeTruthy();
  });
});

describe("signed-in account with unreachable identity", () => {
  const state: AccountState = JSON.parse('{"signedIn":true,"user":null,"error":"Identity service unavailable"}');

  it("keeps the panel signed in and retries the canonical account read", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.accountLogin = vi.fn();
    const reload = vi.fn();
    const { container, rerender } = render(<Account port={port} state={state} reload={reload} />);

    expect(container.querySelector(".acct-who")?.textContent).toBe("已登录");
    expect(screen.getByRole("alert").textContent).toContain(state.error);
    expect(screen.queryByRole("button", { name: "登录" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(reload).toHaveBeenCalledOnce();
    expect(port.accountLogin).not.toHaveBeenCalled();

    const recovered = { signedIn: true, user: { label: "Sample User", handle: "sample", email: "sample@example.test" } };
    rerender(<Account port={port} state={recovered} reload={reload} />);
    expect(screen.getAllByText("Sample User").length).toBeGreaterThan(0);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("button", { name: "重试" })).toBeNull();
  });

  it("allows confirmed sign-out without a user object", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.accountLogout = vi.fn(async () => {});
    const reload = vi.fn();
    render(<Account port={port} state={state} reload={reload} />);
    await userEvent.click(screen.getByRole("button", { name: "退出登录" }));
    expect(port.accountLogout).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "确认退出" }));
    await waitFor(() => expect(port.accountLogout).toHaveBeenCalledOnce());
    expect(reload).toHaveBeenCalledOnce();
  });

  it("shows sign-out failures while retaining the identity error", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.accountLogout = vi.fn(async () => { throw new Error("Credential store unavailable"); });
    const reload = vi.fn();
    render(<Account port={port} state={{ signedIn: true, error: state.error }} reload={reload} />);
    await userEvent.click(screen.getByRole("button", { name: "退出登录" }));
    await userEvent.click(screen.getByRole("button", { name: "确认退出" }));
    expect(await screen.findByText("Credential store unavailable")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toContain(state.error);
    expect(reload).not.toHaveBeenCalled();
  });
});

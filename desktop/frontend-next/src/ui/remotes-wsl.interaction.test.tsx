// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Remotes } from "./Remotes";
import type { RemoteHost, WSLDistros } from "../port/remote";
import type { HubPort } from "../port/hub";

afterEach(cleanup);

const distros: WSLDistros = { available: true, distros: [{ name: "Ubuntu-24.04", version: 2, default: true }, { name: "Old", version: 1 }, { name: "Debian", version: 2 }] };

function draw(over: Partial<HubPort> = {}, hosts: RemoteHost[] = []) {
  const hub = {
    remoteHosts: vi.fn(async () => hosts),
    remoteCandidates: vi.fn(async () => []),
    wslDistros: vi.fn(async () => distros),
    saveRemoteHost: vi.fn(async () => {}),
    removeRemoteHost: vi.fn(async () => {}),
    probeRemote: vi.fn(),
    ...over,
  } as unknown as HubPort;
  const onError = vi.fn();
  render(<Remotes hub={hub} onError={onError} />);
  return { hub, onError };
}

describe("adding a WSL distribution", () => {
  it("offers the choice only where WSL exists", async () => {
    draw({ wslDistros: vi.fn(async () => ({ available: false, distros: [] })) });
    await userEvent.click(await screen.findByRole("button", { name: /添加机器/ }));
    expect(screen.queryByRole("radiogroup", { name: "连接类型" })).toBeNull();
    expect(screen.getByText("地址")).toBeTruthy();
  });

  it("does not break the form when asking about WSL fails", async () => {
    draw({ wslDistros: vi.fn(async () => { throw new Error("down"); }) });
    await userEvent.click(await screen.findByRole("button", { name: /添加机器/ }));
    expect(screen.queryByRole("radiogroup", { name: "连接类型" })).toBeNull();
    expect(screen.getByText("地址")).toBeTruthy();
  });

  it("swaps the SSH fields for a distribution and saves only what a WSL machine has", async () => {
    const { hub } = draw();
    await userEvent.click(await screen.findByRole("button", { name: /添加机器/ }));
    const kinds = await screen.findByRole("radiogroup", { name: "连接类型" });
    await userEvent.click(within(kinds).getByRole("radio", { name: "WSL 发行版" }));
    for (const gone of ["地址", "用户", "端口", "跳板机", "私钥文件", "私钥口令的环境变量名", "登录密码的环境变量名", "模型凭据"]) {
      expect(screen.queryByText(gone), gone).toBeNull();
    }
    expect(screen.queryByText("留空的项将从 ~/.ssh/config 读取")).toBeNull();
    expect(screen.getByText(/关闭 Studio 后它会随发行版一起停止/)).toBeTruthy();

    const select = screen.getByLabelText("发行版") as HTMLSelectElement;
    expect(select.value).toBe("Ubuntu-24.04");
    expect([...select.options].find((o) => o.value === "Old")?.disabled).toBe(true);
    await userEvent.selectOptions(select, "Debian");
    await userEvent.type(screen.getByLabelText("名称"), "dev");
    await userEvent.type(screen.getByPlaceholderText(/远端的项目目录/), "/home/me/app");
    await userEvent.click(within(document.querySelector('[data-add]') as HTMLElement).getByRole("button", { name: "添加" }));
    await userEvent.click(document.querySelector('[data-action="remotes.add"]') as HTMLElement);
    await waitFor(() => expect(hub.saveRemoteHost).toHaveBeenCalledOnce());
    const sent = (hub.saveRemoteHost as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(sent).toMatchObject({ name: "dev", kind: "wsl", distro: "Debian", workspaces: ["/home/me/app"] });
    expect(sent.host).toBe("");
    expect(sent.useSSHConfig).toBe(false);
  });

  it("cannot be saved without a distribution", async () => {
    draw({ wslDistros: vi.fn(async () => ({ available: true, distros: [{ name: "Old", version: 1 }] })) });
    await userEvent.click(await screen.findByRole("button", { name: /添加机器/ }));
    await userEvent.click(within(await screen.findByRole("radiogroup", { name: "连接类型" })).getByRole("radio", { name: "WSL 发行版" }));
    await userEvent.type(screen.getByLabelText("名称"), "dev");
    expect((document.querySelector('[data-action="remotes.add"]') as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("a saved WSL machine", () => {
  const dev: RemoteHost = { name: "dev", kind: "wsl", distro: "Ubuntu-24.04", target: "wsl:Ubuntu-24.04", status: "idle", provider: "remote", workspaces: ["/home/me/app"] };

  it("is labelled as one and edited without the way it is reached being changeable", async () => {
    const { hub } = draw({}, [dev]);
    expect(await screen.findByText("wsl:Ubuntu-24.04")).toBeTruthy();
    expect(screen.getByText("WSL 发行版")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: /^更多：/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "编辑" }));
    expect(screen.queryByRole("radiogroup", { name: "连接类型" })).toBeNull();
    expect((screen.getByLabelText("发行版") as HTMLSelectElement).disabled).toBe(true);
    expect(screen.queryByText("地址")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(hub.saveRemoteHost).toHaveBeenCalledOnce());
    expect((hub.saveRemoteHost as ReturnType<typeof vi.fn>).mock.calls[0][0]).toMatchObject({ name: "dev", kind: "wsl", distro: "Ubuntu-24.04" });
  });

  it("keeps its row when the menu turns it off, sending the whole row back", async () => {
    const { hub } = draw({}, [dev]);
    await userEvent.click(await screen.findByRole("button", { name: /^更多：/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: /停用这台主机/ }));
    await waitFor(() => expect(hub.saveRemoteHost).toHaveBeenCalledOnce());
    expect((hub.saveRemoteHost as ReturnType<typeof vi.fn>).mock.calls[0][0]).toMatchObject({ name: "dev", kind: "wsl", distro: "Ubuntu-24.04", disabled: true });
  });
});

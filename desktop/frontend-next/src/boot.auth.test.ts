// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import HTML from "../index.html?raw";

const script = new DOMParser().parseFromString(HTML, "text/html").querySelector("script")!.textContent!;

function boot(fragment: string, path = "/", search = "") {
  let resolve!: (response: { ok: boolean }) => void;
  let reject!: (error: Error) => void;
  const response = new Promise<{ ok: boolean }>((yes, no) => { resolve = yes; reject = no; });
  const fetch = vi.fn((_url: string, _options?: unknown) => response);
  const reload = vi.fn();
  const replaceState = vi.fn();
  const context = {
    window: null as unknown,
    __rxAuth: undefined as Promise<void> | undefined,
    fetch,
    location: { hash: fragment, pathname: path, search, reload },
    history: { replaceState },
    localStorage: { getItem: () => null },
    document: { documentElement: { dataset: {}, lang: "" } },
    navigator: { language: "en" },
    performance: { mark: vi.fn() },
    URLSearchParams,
  };
  context.window = context;
  new Function(...Object.keys(context), script)(...Object.values(context));
  return { context, fetch, reload, replaceState, resolve, reject };
}

describe("credential bootstrap before native asset loading", () => {
  it.each([
    ["token", "#token=launch-secret", "/auth/token", { token: "launch-secret" }],
    ["pair", "#pair=one-time-code", "/pair", { code: "one-time-code" }],
  ])("reloads after the %s cookie is established", async (_kind, fragment, route, body) => {
    const page = boot(fragment);
    expect(page.fetch).toHaveBeenCalledWith(route, {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body),
    });
    expect(page.reload).not.toHaveBeenCalled();
    page.resolve({ ok: true });
    await page.context.__rxAuth;
    expect(page.reload).toHaveBeenCalledOnce();
  });

  it("preserves the session path, query, and unrelated fragment before reloading", async () => {
    const page = boot("#tab=extensions&token=secret", "/sessions/session-123", "?layout=wide");
    expect(page.replaceState).toHaveBeenCalledWith(null, "", "/sessions/session-123?layout=wide#tab=extensions");
    expect(page.replaceState.mock.calls[0][2]).not.toContain("secret");
    page.resolve({ ok: true });
    await page.context.__rxAuth;
    expect(page.reload).toHaveBeenCalledOnce();
  });

  it("consumes pairing once when both credentials are present", async () => {
    const page = boot("#token=secret&pair=once");
    expect(page.fetch).toHaveBeenCalledWith("/pair", expect.objectContaining({ body: JSON.stringify({ code: "once" }) }));
    expect(page.replaceState).toHaveBeenCalledWith(null, "", "/");
    page.resolve({ ok: true });
    await page.context.__rxAuth;
    expect(page.fetch).toHaveBeenCalledOnce();
    expect(page.reload).toHaveBeenCalledOnce();
  });

  it("keeps application fetches behind the existing authentication promise", async () => {
    const page = boot("#token=secret");
    const application = page.context.fetch("/runtimes");
    expect(page.fetch).toHaveBeenCalledOnce();
    page.resolve({ ok: true });
    await application;
    expect(page.fetch).toHaveBeenLastCalledWith("/runtimes");
    expect(page.reload).toHaveBeenCalledOnce();
  });

  it.each(["", "#tab=extensions"])("does not reload a page without a credential (%s)", async (fragment) => {
    const page = boot(fragment);
    await page.context.__rxAuth;
    expect(page.fetch).not.toHaveBeenCalled();
    expect(page.replaceState).not.toHaveBeenCalled();
    expect(page.reload).not.toHaveBeenCalled();
  });

  it.each(["#token=wrong", "#pair=expired"])("does not reload a refused exchange (%s)", async (fragment) => {
    const page = boot(fragment);
    page.resolve({ ok: false });
    await page.context.__rxAuth;
    expect(page.reload).not.toHaveBeenCalled();
  });

  it("does not reload when the exchange cannot reach the server", async () => {
    const page = boot("#token=secret");
    const failure = new Error("network unavailable");
    page.reject(failure);
    await expect(page.context.__rxAuth).rejects.toBe(failure);
    expect(page.reload).not.toHaveBeenCalled();
  });
});

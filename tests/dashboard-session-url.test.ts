import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { readSessionFromUrl, writeSessionToUrl } from "../dashboard/src/lib/session-url";

type FakeWindow = {
  location: { href: string; search: string };
  history: { replaceState: (s: unknown, t: string, url: string) => void };
};

function installFakeWindow(href: string): FakeWindow {
  const fake: FakeWindow = {
    location: { href, search: new URL(href).search },
    history: {
      replaceState: (_state, _title, url) => {
        const next = new URL(url, fake.location.href);
        fake.location.href = next.href;
        fake.location.search = next.search;
      },
    },
  };
  (globalThis as unknown as { window: FakeWindow }).window = fake;
  return fake;
}

describe("dashboard session URL helper", () => {
  beforeEach(() => {
    (globalThis as { window?: unknown }).window = undefined;
  });
  afterEach(() => {
    (globalThis as { window?: unknown }).window = undefined;
  });

  it("returns null when no window (SSR-safe)", async () => {
    expect(readSessionFromUrl()).toBeNull();
  });

  it("returns null when ?session is absent", async () => {
    installFakeWindow("http://localhost:3000/?token=abc");

    expect(readSessionFromUrl()).toBeNull();
  });

  it("returns the session name when ?session=NAME present", async () => {
    installFakeWindow("http://localhost:3000/?token=abc&session=foo");

    expect(readSessionFromUrl()).toBe("foo");
  });

  it("returns null when ?session= is blank", async () => {
    installFakeWindow("http://localhost:3000/?token=abc&session=");

    expect(readSessionFromUrl()).toBeNull();
  });

  it("decodes CJK / spaces in session names", async () => {
    installFakeWindow("http://localhost:3000/?session=%E4%BC%9A%E8%AF%9D%201");

    expect(readSessionFromUrl()).toBe("会话 1");
  });

  it("writes session param while preserving token", async () => {
    const fake = installFakeWindow("http://localhost:3000/?token=abc");

    writeSessionToUrl("foo");
    expect(fake.location.href).toContain("token=abc");
    expect(fake.location.href).toContain("session=foo");
  });

  it("replaces an existing session param", async () => {
    const fake = installFakeWindow("http://localhost:3000/?token=abc&session=old");

    writeSessionToUrl("new");
    expect(fake.location.href).toContain("session=new");
    expect(fake.location.href).not.toContain("session=old");
  });

  it("clears the session param when passed null", async () => {
    const fake = installFakeWindow("http://localhost:3000/?token=abc&session=foo");

    writeSessionToUrl(null);
    expect(fake.location.href).not.toContain("session=");
    expect(fake.location.href).toContain("token=abc");
  });

  it("treats blank/whitespace name as clear", async () => {
    const fake = installFakeWindow("http://localhost:3000/?token=abc&session=foo");

    writeSessionToUrl("   ");
    expect(fake.location.href).not.toContain("session=");
  });

  it("is a no-op when the URL is already correct", async () => {
    const fake = installFakeWindow("http://localhost:3000/?token=abc&session=foo");
    let callCount = 0;
    const original = fake.history.replaceState;
    fake.history.replaceState = (...args) => {
      callCount += 1;
      original(...args);
    };

    writeSessionToUrl("foo");
    expect(callCount).toBe(0);
  });
});

// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { normalizeHiddenHosts, onHiddenHostsChange, readHiddenHosts, toggleHiddenHost, writeHiddenHosts } from "./remotehide";

beforeEach(() => {
  localStorage.clear();
  writeHiddenHosts([]);
});

afterEach(() => {
  localStorage.clear();
});

describe("the hidden-remote-hosts preference", () => {
  it("normalizes whatever was stored", () => {
    expect(normalizeHiddenHosts(["a", "a", " b ", "", 7])).toEqual(["a", "b"]);
    expect(normalizeHiddenHosts("nope")).toEqual([]);
    expect(normalizeHiddenHosts(undefined)).toEqual([]);
  });

  it("round-trips through storage", () => {
    expect(writeHiddenHosts(["build-box"])).toBe(true);
    expect(readHiddenHosts()).toEqual(["build-box"]);
  });

  it("toggles one machine and leaves the others where they were", () => {
    writeHiddenHosts(["a"]);
    toggleHiddenHost("b");
    expect(readHiddenHosts()).toEqual(["a", "b"]);
    toggleHiddenHost("a");
    expect(readHiddenHosts()).toEqual(["b"]);
  });

  it("notifies subscribers only when the set actually changed", () => {
    const seen = vi.fn();
    const stop = onHiddenHostsChange(seen);
    writeHiddenHosts(["a"]);
    expect(seen).toHaveBeenCalledTimes(1);
    writeHiddenHosts(["a"]);
    expect(seen).toHaveBeenCalledTimes(1);
    toggleHiddenHost("a");
    expect(seen).toHaveBeenCalledTimes(2);
    stop();
  });

  it("survives a stored value that is not a list", () => {
    localStorage.setItem("rx-hidden-remote-hosts", "{ not json");
    expect(readHiddenHosts()).toEqual([]);
  });
});

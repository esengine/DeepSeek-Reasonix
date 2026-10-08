import assert from "node:assert/strict";
import test from "node:test";
import { detectDesktopOS, detectPlatform, detectWindowsArm64 } from "./os-detect.js";

const UA = {
  win: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130 Safari/537.36",
  winArm: "Mozilla/5.0 (Windows NT 10.0; ARM64) AppleWebKit/537.36 Chrome/130 Safari/537.36",
  mac: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17 Safari/605.1.15",
  linux: "Mozilla/5.0 (X11; Linux x86_64) Firefox/130",
  linuxArm: "Mozilla/5.0 (X11; Linux aarch64) Firefox/130",
  linuxArm32: "Mozilla/5.0 (X11; Linux armv7l) Firefox/130",
  chromeOS: "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 Chrome/130 Safari/537.36",
  iphone: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148",
  ipad: "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) Mobile/15E148",
  androidPhone: "Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome/130 Mobile Safari/537.36",
  androidTablet: "Mozilla/5.0 (Linux; Android 14; SM-X700) AppleWebKit/537.36 Chrome/130 Safari/537.36",
};

test("desktop platforms with a shipped build", () => {
  assert.equal(detectDesktopOS(UA.win), "win");
  assert.equal(detectDesktopOS(UA.mac), "mac");
  assert.equal(detectDesktopOS(UA.mac, 0), "mac");
  assert.equal(detectDesktopOS(UA.linux), "linux");
});

test("Windows on ARM keeps the Windows x64 installer", () => {
  assert.deepEqual(detectPlatform(UA.winArm), { os: "win", reason: "" });
});

test("iPadOS 13+ Safari (Macintosh UA with a touch screen) is a tablet", () => {
  assert.deepEqual(detectPlatform(UA.mac, 5), { os: "", reason: "mobile" });
  assert.deepEqual(detectPlatform(UA.mac, 2), { os: "", reason: "mobile" });
  assert.equal(detectDesktopOS(UA.mac, 1), "mac");
});

test("Android tablets without Mobile in the UA are not Linux desktops", () => {
  assert.deepEqual(detectPlatform(UA.androidTablet), { os: "", reason: "mobile" });
  assert.deepEqual(detectPlatform(UA.androidPhone), { os: "", reason: "mobile" });
});

test("iPhone and iPad UAs are mobile", () => {
  assert.equal(detectPlatform(UA.iphone).reason, "mobile");
  assert.equal(detectPlatform(UA.ipad).reason, "mobile");
});

test("ChromeOS gets no recommendation, not the Linux .deb", () => {
  assert.deepEqual(detectPlatform(UA.chromeOS), { os: "", reason: "chromeos" });
});

test("Linux on ARM gets no amd64 recommendation", () => {
  assert.deepEqual(detectPlatform(UA.linuxArm), { os: "", reason: "arm-linux" });
  assert.deepEqual(detectPlatform(UA.linuxArm32), { os: "", reason: "arm-linux" });
  assert.equal(detectDesktopOS(UA.linux), "linux");
});

test("unknown agents and missing UAs show every build", () => {
  assert.deepEqual(detectPlatform("curl/8.0"), { os: "", reason: "unknown" });
  assert.deepEqual(detectPlatform(undefined), { os: "", reason: "unknown" });
});

const hints = (platform, architecture, bitness) => ({
  platform,
  getHighEntropyValues: async () => ({ architecture, bitness }),
});

test("Windows ARM64 is told apart only through client hints", async () => {
  assert.equal(await detectWindowsArm64(hints("Windows", "arm", "64")), true);
  assert.equal(await detectWindowsArm64(hints("Windows", "x86", "64")), false);
  assert.equal(await detectWindowsArm64(hints("macOS", "arm", "64")), false);
  assert.equal(await detectWindowsArm64(undefined), false);
  assert.equal(await detectWindowsArm64({ platform: "Windows" }), false);
  assert.equal(await detectWindowsArm64({
    platform: "Windows",
    getHighEntropyValues: async () => { throw new Error("blocked"); },
  }), false);
});

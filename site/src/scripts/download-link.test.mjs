import { test } from "node:test";
import assert from "node:assert/strict";
import { downloadPaneFromURL, downloadURLForPane, releaseChannelFromURL } from "./download-link.js";

test("desktop updater deep link selects the desktop pane", () => {
  assert.equal(downloadPaneFromURL("https://reasonix.io/?download=desktop#start"), "desktop");
});

test("plain install links preserve the default pane", () => {
  assert.equal(downloadPaneFromURL("https://reasonix.io/#start"), "");
  assert.equal(downloadPaneFromURL("https://reasonix.io/?download=desktop"), "");
});

test("recognized download panes are strict", () => {
  assert.equal(downloadPaneFromURL("/?download=brew#start"), "brew");
  assert.equal(downloadPaneFromURL("/?download=npm#start"), "npm");
  assert.equal(downloadPaneFromURL("/?download=vscode#start"), "vscode");
  assert.equal(downloadPaneFromURL("/?download=cli#start"), "cli");
  assert.equal(downloadPaneFromURL("/?download=DESKTOP#start"), "");
  assert.equal(downloadPaneFromURL("/?download=unknown#start"), "");
  assert.equal(downloadPaneFromURL("not a url", "not a base"), "");
});

test("release channel deep links apply only to Desktop and native CLI", () => {
  assert.equal(releaseChannelFromURL("/?download=desktop&channel=preview#start"), "preview");
  assert.equal(releaseChannelFromURL("/?download=cli&channel=stable#start"), "stable");
  assert.equal(releaseChannelFromURL("/?download=npm&channel=preview#start"), "");
  assert.equal(releaseChannelFromURL("/?download=desktop&channel=canary#start"), "");
  assert.equal(releaseChannelFromURL("/?download=desktop&channel=preview"), "");
});

test("download tab navigation keeps the visible pane and channel in the URL", () => {
  assert.equal(
    downloadURLForPane("https://reasonix.io/?download=desktop&channel=preview#start", "cli", "stable"),
    "https://reasonix.io/?download=cli&channel=stable#start",
  );
  assert.equal(
    downloadURLForPane("https://reasonix.io/?download=desktop&channel=preview#start", "npm", "preview"),
    "https://reasonix.io/?download=npm#start",
  );
  assert.equal(downloadURLForPane("https://reasonix.io/", "desktop", "preview"),
    "https://reasonix.io/?download=desktop&channel=preview#start");
  assert.equal(downloadURLForPane("https://reasonix.io/", "unknown", "stable"), "");
});

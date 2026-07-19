import { test } from "node:test";
import assert from "node:assert/strict";
import { downloadPaneFromURL } from "./download-link.js";

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
  assert.equal(downloadPaneFromURL("/?download=DESKTOP#start"), "");
  assert.equal(downloadPaneFromURL("/?download=unknown#start"), "");
  assert.equal(downloadPaneFromURL("not a url", "not a base"), "");
});

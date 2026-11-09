// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { DataSettings } from "./DataSettings";

afterEach(cleanup);

function draw(signedIn = false) {
  const port = new MockPort() as unknown as AgentPort;
  render(
    <>
      <DataSettings port={port} homePort={port} localPort={port} hub={new MockHub()} workspace="/w" cwd="/w" busy="" signedIn={signedIn}
        onIsolate={() => {}} onChanged={() => {}} onRecovered={() => {}} />
    </>,
  );
}

it("is three cards: memory and usage, session storage, backup, each setting one row", async () => {
  draw();
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["记忆与用量", "会话存储", "备份"]);
  for (const id of ["memory", "usage", "upload-usage", "currency", "storage", "auto-archive", "session-dir", "legacy-sessions", "backup-file"]) {
    await waitFor(() => expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy());
  }
  expect(document.querySelector('[data-setting="backup"]')).toBeNull();
});

it("expands memory, usage, storage and export controls together", async () => {
  draw();
  await waitFor(() => {
    for (const id of ["memory", "usage", "storage", "backup-file"]) {
      expect(document.querySelector(`#set-${id} .grp-items`), id).toBeTruthy();
    }
  });
});

it("offers the cloud backup only when signed in", () => {
  draw(true);
  expect(document.querySelector('[data-setting="backup"]')).toBeTruthy();
});

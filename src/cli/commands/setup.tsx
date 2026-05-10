/**
 * `reasonix setup` — re-mount the first-run wizard on demand so users
 * can reconfigure (add/remove MCP servers, switch preset) without
 * editing JSON by hand.
 *
 * Invoked both explicitly (`reasonix setup`) and implicitly (the no-args
 * entry point when `setupCompleted` is false).
 */

import { render } from "ink";
import React from "react";
import { loadApiKey, readConfig } from "../../config.js";
import { loadDotenv } from "../../env.js";
import { Wizard } from "../ui/Wizard.js";

export interface SetupOptions {
  /**
   * When true, bypass the API-key step even if no key is saved — useful
   * from test harnesses. Normal CLI use always pushes through the key
   * step when missing.
   */
  skipKeyStep?: boolean;
  /** Show the API-key step even when a saved/env key already exists. */
  forceKeyStep?: boolean;
}

export async function setupCommand(opts: SetupOptions = {}): Promise<void> {
  loadDotenv();
  const existingKey = loadApiKey();
  const existing = readConfig();

  const { waitUntilExit, unmount } = render(
    <Wizard
      existingApiKey={existingKey}
      initial={{ preset: existing.preset, mcp: existing.mcp, theme: existing.theme }}
      forceApiKeyStep={opts.forceKeyStep}
      onComplete={() => {
        // Ink handles its own enter-to-exit inside the "saved" step; we
        // just wait for the app to exit naturally.
      }}
      onCancel={() => {
        unmount();
      }}
    />,
    { exitOnCtrlC: true, patchConsole: false },
  );
  await waitUntilExit();
}

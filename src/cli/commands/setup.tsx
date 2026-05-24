/** `reasonix setup` — re-mount the first-run wizard. */

import { render } from "ink";
import React from "react";
import { loadApiKey, readConfig } from "../../config.js";
import { loadDotenv } from "../../env.js";
import { Wizard } from "../ui/Wizard.js";

export interface SetupOptions {
  /** Test-only — skip the API-key step. */
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
      initial={{ mcp: existing.mcp, theme: existing.theme }}
      forceApiKeyStep={opts.forceKeyStep}
      onComplete={() => undefined}
      onCancel={() => {
        unmount();
      }}
    />,
    { exitOnCtrlC: true, patchConsole: false },
  );
  await waitUntilExit();
}

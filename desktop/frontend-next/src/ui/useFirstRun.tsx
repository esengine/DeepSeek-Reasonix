import { useState, type ReactNode } from "react";
import type { AgentPort, ProviderSetup } from "../port/port";
import type { ConnectOffer } from "./ConnectNote";
import { Onboarding } from "./Onboarding";
import type { ProjectChoice } from "./OnboardingProject";

interface Args {
  port: AgentPort | null | undefined;
  setup: ProviderSetup | null | undefined;
  welcomed: boolean | undefined;
  setSetup: (next: ProviderSetup | null) => void;
  setWelcomed: (next: boolean) => void;
  reloadPanes: () => Promise<unknown> | void;
  project?: ProjectChoice;
}

// The key prompt is owed until a provider exists, but putting it off is the
// person's call for this launch only: the deferral lives in memory, so the next
// start asks again.
export function useFirstRun({ port, setup, welcomed, setSetup, setWelcomed, reloadPanes, project }: Args): { gate: ReactNode; connect?: ConnectOffer } {
  const [deferred, setDeferred] = useState(false);
  if (!setup?.required || welcomed === undefined || !port) return { gate: null };
  if (deferred) return { gate: null, connect: { none: !!setup.noProvider, open: () => setDeferred(false) } };

  const settleWelcome = () => {
    if (welcomed) return;
    setWelcomed(true);
    void port.markWelcomed().catch(() => {});
  };
  return {
    gate: (
      <Onboarding
        port={port}
        setup={setup}
        project={project}
        onSkip={() => {
          setDeferred(true);
          settleWelcome();
        }}
        onDone={() => {
          setSetup(null);
          settleWelcome();
          void reloadPanes();
        }}
      />
    ),
  };
}

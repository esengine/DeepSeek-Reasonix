import { errorText, type Logger } from "./log.js";

export type QuitPhase = "idle" | "asking" | "shutting-down" | "done";

export interface LifecycleService {
  beforeClose(reason: string): Promise<boolean>;
  shutdown(): Promise<void>;
}

export interface LifecycleApp {
  quit(): void;
  relaunch(args: string[], execPath?: string): void;
}

export interface QuitSequencerDeps {
  service: LifecycleService;
  app: LifecycleApp;
  onCloseAllowed(): void;
  log: Logger;
}

// Electron's before-quit fires on every app.quit(); this drives it through
// beforeClose (Go may veto) and shutdown exactly once, then lets it through.
export class QuitSequencer {
  private phase: QuitPhase = "idle";
  private approved = false;
  private relaunchArgs: string[] | null = null;
  private relaunchExecPath: string | undefined;

  constructor(private readonly deps: QuitSequencerDeps) {}

  get currentPhase(): QuitPhase {
    return this.phase;
  }

  onBeforeQuit(): boolean {
    if (this.phase === "done") return true;
    if (this.phase !== "idle") return false;
    if (!this.approved) {
      this.phase = "asking";
      void this.ask();
      return false;
    }
    this.phase = "shutting-down";
    void this.finish();
    return false;
  }

  requestQuit(): void {
    this.deps.app.quit();
  }

  approve(): void {
    this.approved = true;
    this.deps.app.quit();
  }

  relaunch(args: string[], execPath?: string): void {
    this.relaunchArgs = args;
    this.relaunchExecPath = execPath;
    this.approve();
  }

  private async ask(): Promise<void> {
    let prevent = false;
    try {
      prevent = await this.deps.service.beforeClose("quit");
    } catch (error) {
      this.deps.log.warn(`beforeClose(quit) failed, quitting anyway: ${errorText(error)}`);
    }
    this.phase = "idle";
    if (prevent) return;
    this.approved = true;
    this.deps.app.quit();
  }

  private async finish(): Promise<void> {
    try {
      await this.deps.service.shutdown();
    } catch (error) {
      this.deps.log.warn(`shutdown failed: ${errorText(error)}`);
    }
    this.phase = "done";
    this.deps.onCloseAllowed();
    if (this.relaunchArgs) this.deps.app.relaunch(this.relaunchArgs, this.relaunchExecPath);
    this.deps.app.quit();
  }
}

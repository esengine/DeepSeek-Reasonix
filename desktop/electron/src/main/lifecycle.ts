import { errorText, type Logger } from "./log.js";
import { randomUUID } from "node:crypto";

export type QuitPhase = "idle" | "asking" | "shutting-down" | "done";

export interface LifecycleService {
  beforeClose(reason: string): Promise<boolean>;
  shutdown(): Promise<void>;
}

export interface LifecycleApp {
  quit(): void;
  exit?(code: number): void;
  relaunch(args: string[], execPath?: string): void;
}

export interface QuitSequencerDeps {
  service: LifecycleService;
  app: LifecycleApp;
  onCloseAllowed(): void;
  cleanup?: Array<{ name: string; run(): void }>;
  schedule?: (run: () => void, milliseconds: number) => void;
  log: Logger;
}

// Electron's before-quit fires on every app.quit(); this drives it through
// beforeClose (Go may veto) and shutdown exactly once, then lets it through.
export class QuitSequencer {
  private phase: QuitPhase = "idle";
  private approved = false;
  private relaunchArgs: string[] | null = null;
  private relaunchExecPath: string | undefined;
  private attempt = "";

  constructor(private readonly deps: QuitSequencerDeps) {}

  get currentPhase(): QuitPhase {
    return this.phase;
  }

  get isQuitting(): boolean { return this.approved || this.phase === "shutting-down" || this.phase === "done"; }

  onBeforeQuit(): boolean {
    if (this.phase === "done") return true;
    if (this.phase !== "idle") return false;
    if (!this.attempt) this.attempt = randomUUID();
    this.deps.log.info(`exit ${this.attempt}: ${this.approved ? "shutting-down" : "asking"}`);
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
    if (prevent && !this.approved) { this.deps.log.info(`exit ${this.attempt}: cancelled`); this.attempt = ""; return; }
    this.approved = true;
    this.deps.app.quit();
  }

  private async finish(): Promise<void> {
    try {
      await this.deps.service.shutdown();
    } catch (error) {
      this.deps.log.warn(`exit ${this.attempt}: shutdown failed: ${errorText(error)}`);
      this.phase = "idle";
      this.approved = false;
      return;
    }
    for (const step of [{ name: "close permission", run: () => this.deps.onCloseAllowed() }, ...(this.deps.cleanup ?? [])]) {
      try { step.run(); this.deps.log.info(`exit ${this.attempt}: cleanup ${step.name} complete`); } catch (error) { this.deps.log.warn(`exit ${this.attempt}: cleanup ${step.name} failed: ${errorText(error)}`); }
    }
    this.phase = "done";
    this.deps.log.info(`exit ${this.attempt}: resources cleaned; requesting final shell exit`);
    if (this.deps.app.exit) {
      const schedule = this.deps.schedule ?? ((run, ms) => { setTimeout(run, ms).unref(); });
      schedule(() => {
        this.deps.log.error("shell exit deadline exceeded after service shutdown");
        this.deps.app.exit?.(1);
      }, 5000);
    }
    try {
      if (this.relaunchArgs) this.deps.app.relaunch(this.relaunchArgs, this.relaunchExecPath);
    } catch (error) {
      this.deps.log.error(`relaunch failed: ${errorText(error)}`);
    } finally { this.deps.app.quit(); }
  }
}

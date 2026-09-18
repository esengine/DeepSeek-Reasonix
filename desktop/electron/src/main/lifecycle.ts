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
  flushRenderer?: () => Promise<void>;
  resumeRenderer?: () => Promise<void>;
  onCloseAllowed(): void;
  onClosePrevented?(reason: string): void;
  cleanup?: Array<{ name: string; run(): void }>;
  schedule?: (run: () => void, milliseconds: number) => void;
  log: Logger;
}

// Every close entry point joins one decision/shutdown promise. A veto resets the
// sequencer only after beforeClose completes; accepted shutdown remains single-owner.
export class QuitSequencer {
  private phase: QuitPhase = "idle";
  private approved = false;
  private relaunchArgs: string[] | null = null;
  private relaunchExecPath: string | undefined;
  private attempt = "";
  private pending: Promise<void> | null = null;

  constructor(private readonly deps: QuitSequencerDeps) {}

  get currentPhase(): QuitPhase { return this.phase; }
  get isQuitting(): boolean { return this.approved || this.phase === "shutting-down" || this.phase === "done"; }

  onBeforeQuit(): boolean {
    if (this.phase === "done") return true;
    this.begin("quit", this.approved);
    return false;
  }

  requestQuit(): void { this.deps.app.quit(); }
  requestClose(reason = "window"): void { this.begin(reason, false); }
  approve(): void { this.approved = true; this.begin("approved", true); }

  relaunch(args: string[], execPath?: string): void {
    this.relaunchArgs = args;
    this.relaunchExecPath = execPath;
    this.approve();
  }

  private begin(reason: string, approved: boolean): void {
    if (this.phase === "done" || this.pending) return;
    if (!this.attempt) this.attempt = randomUUID();
    this.approved ||= approved;
    const stage = this.approved ? "shutting-down" : "asking (" + reason + ")";
    this.deps.log.info("exit " + this.attempt + ": " + stage);
    this.pending = (this.approved ? this.finish() : this.ask(reason)).finally(() => { this.pending = null; });
  }

  private async ask(reason: string): Promise<void> {
    this.phase = "asking";
    let prevent = false;
    try {
      await this.deps.flushRenderer?.();
    } catch (error) {
      this.deps.log.warn(`exit ${this.attempt}: draft flush failed; quit cancelled: ${errorText(error)}`);
      this.phase = "idle";
      this.approved = false;
      this.attempt = "";
      return;
    }
    try {
      prevent = await this.deps.service.beforeClose(reason);
    } catch (error) {
      this.deps.log.warn("beforeClose(" + reason + ") failed, closing anyway: " + errorText(error));
    }
    if (prevent && !this.approved) {
      this.deps.log.info("exit " + this.attempt + ": cancelled");
      this.phase = "idle";
      this.attempt = "";
      await this.resumeRenderer();
      this.deps.onClosePrevented?.(reason);
      return;
    }
    this.approved = true;
    await this.finish();
  }

  private async finish(): Promise<void> {
    this.phase = "shutting-down";
    try {
      await this.deps.flushRenderer?.();
    } catch (error) {
      this.deps.log.warn(`exit ${this.attempt}: draft flush failed; shutdown cancelled: ${errorText(error)}`);
      this.phase = "idle";
      this.approved = false;
      return;
    }
    try {
      await this.deps.service.shutdown();
    } catch (error) {
      this.deps.log.warn("exit " + this.attempt + ": shutdown failed: " + errorText(error));
      this.phase = "idle";
      this.approved = false;
      await this.resumeRenderer();
      return;
    }
    for (const step of [{ name: "close permission", run: () => this.deps.onCloseAllowed() }, ...(this.deps.cleanup ?? [])]) {
      try { step.run(); this.deps.log.info("exit " + this.attempt + ": cleanup " + step.name + " complete"); }
      catch (error) { this.deps.log.warn("exit " + this.attempt + ": cleanup " + step.name + " failed: " + errorText(error)); }
    }
    this.phase = "done";
    this.deps.log.info("exit " + this.attempt + ": resources cleaned; requesting final shell exit");
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
      this.deps.log.error("relaunch failed: " + errorText(error));
    } finally { this.deps.app.quit(); }
  }

  private async resumeRenderer(): Promise<void> {
    try {
      await this.deps.resumeRenderer?.();
    } catch (error) {
      this.deps.log.warn(`exit ${this.attempt}: could not resume draft editing: ${errorText(error)}`);
    }
  }
}

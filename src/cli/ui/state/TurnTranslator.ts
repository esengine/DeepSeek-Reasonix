import type { TurnStats } from "../../../telemetry/stats.js";
import type { Scrollback } from "../hooks/useScrollback.js";

export class TurnTranslator {
  private reasoningCardId: string | null = null;
  private streamingCardId: string | null = null;
  private toolCardId: string | null = null;
  private toolStartedAt = 0;

  constructor(private readonly log: Scrollback) {}

  flushBuffers(reasoningChunk: string, contentChunk: string, model?: string): void {
    if (reasoningChunk) {
      if (!this.reasoningCardId) this.reasoningCardId = this.log.startReasoning(model);
      this.log.appendReasoning(this.reasoningCardId, reasoningChunk);
    }
    if (contentChunk) {
      if (!this.streamingCardId) this.streamingCardId = this.log.startStreaming(model);
      this.log.appendStreaming(this.streamingCardId, contentChunk);
    }
  }

  toolStart(name: string, args: unknown): void {
    this.toolStartedAt = Date.now();
    this.toolCardId = this.log.startTool(name, args);
  }

  toolEnd(output: string): void {
    if (this.toolCardId) {
      this.log.endTool(this.toolCardId, {
        output,
        elapsedMs: Date.now() - this.toolStartedAt,
      });
      this.toolCardId = null;
    }
  }

  toolRetry(attempt: number, max: number): void {
    if (this.toolCardId) this.log.retryTool(this.toolCardId, attempt, max);
  }

  reasoningDone(reasoningText: string): void {
    if (!this.reasoningCardId) return;
    const paragraphs = reasoningText ? reasoningText.split(/\n\s*\n/).length : 0;
    const tokens = Math.round(reasoningText.length / 4);
    this.log.endReasoning(this.reasoningCardId, paragraphs, tokens);
    this.reasoningCardId = null;
  }

  streamingDone(): void {
    if (!this.streamingCardId) return;
    this.log.endStreaming(this.streamingCardId);
    this.streamingCardId = null;
  }

  turnEnd(
    stats: TurnStats,
    reasoningText: string,
    extras?: { promptCap?: number; elapsedMs?: number },
  ): void {
    this.log.endTurn(
      {
        prompt: stats.usage.promptTokens,
        reason: Math.round(reasoningText.length / 4),
        output: stats.usage.completionTokens,
        cacheHit: stats.cacheHitRatio,
        cost: stats.cost,
      },
      extras,
    );
  }

  abort(): void {
    if (this.streamingCardId) {
      this.log.endStreaming(this.streamingCardId, true);
      this.streamingCardId = null;
    }
    if (this.reasoningCardId) {
      this.log.endReasoning(this.reasoningCardId, 0, 0, true);
      this.reasoningCardId = null;
    }
    if (this.toolCardId) {
      this.log.endTool(this.toolCardId, {
        elapsedMs: Date.now() - this.toolStartedAt,
        aborted: true,
      });
      this.toolCardId = null;
    }
    this.log.abortTurn();
  }
}

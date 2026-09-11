import type { TranscriptKernel } from "./transcriptKernel";

/** Source-session data work survives navigation cancellation, but not replacement. */
export class TranscriptHistoryRequest {
  private history: { generation: number; result: Promise<boolean> } | null = null;
  constructor(private readonly kernel: Pick<TranscriptKernel, "generation">) {}

  load(load: () => boolean | Promise<boolean>): Promise<boolean> {
    const generation = this.kernel.generation;
    if (this.history?.generation === generation) return this.history.result;
    const request = { generation, result: Promise.resolve(false) };
    this.history = request;
    request.result = Promise.resolve().then(() => generation === this.kernel.generation && load()).then(
      (loaded) => loaded && generation === this.kernel.generation,
      () => false,
    ).finally(() => {
      if (this.history === request) this.history = null;
    });
    return request.result;
  }
}

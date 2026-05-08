import { Writable } from "node:stream";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  _resetForTests,
  dumpStartupProfile,
  isStartupProfileEnabled,
  markPhase,
} from "../src/cli/startup-profile.js";

function makeSink(): { stream: NodeJS.WriteStream; output: () => string } {
  const chunks: Buffer[] = [];
  const stream = new Writable({
    write(chunk, _enc, cb) {
      chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
      cb();
    },
  }) as unknown as NodeJS.WriteStream;
  return { stream, output: () => Buffer.concat(chunks).toString("utf8") };
}

describe("startup-profile", () => {
  const original = process.env.REASONIX_PROFILE_STARTUP;

  beforeEach(() => {
    _resetForTests();
  });

  afterEach(() => {
    if (original === undefined) Reflect.deleteProperty(process.env, "REASONIX_PROFILE_STARTUP");
    else process.env.REASONIX_PROFILE_STARTUP = original;
    _resetForTests();
  });

  it("is disabled by default — markPhase + dumpStartupProfile are no-ops", () => {
    Reflect.deleteProperty(process.env, "REASONIX_PROFILE_STARTUP");
    expect(isStartupProfileEnabled()).toBe(false);
    markPhase("a");
    markPhase("b");
    const sink = makeSink();
    dumpStartupProfile(sink.stream);
    expect(sink.output()).toBe("");
  });

  it("recognizes 1 / true / yes as enable values", () => {
    for (const v of ["1", "true", "yes"]) {
      process.env.REASONIX_PROFILE_STARTUP = v;
      expect(isStartupProfileEnabled()).toBe(true);
    }
    process.env.REASONIX_PROFILE_STARTUP = "0";
    expect(isStartupProfileEnabled()).toBe(false);
  });

  it("emits a formatted profile when enabled, with cumulative + delta per phase", () => {
    process.env.REASONIX_PROFILE_STARTUP = "1";
    markPhase("first");
    markPhase("second");
    markPhase("third");
    const sink = makeSink();
    dumpStartupProfile(sink.stream);
    const out = sink.output();
    expect(out).toContain("[startup-profile]");
    expect(out).toContain("first");
    expect(out).toContain("second");
    expect(out).toContain("third");
    expect(out).toMatch(/total/);
    expect(out).toMatch(/last phase third/);
  });

  it("dumpStartupProfile is idempotent — second call is silent", () => {
    process.env.REASONIX_PROFILE_STARTUP = "1";
    markPhase("only");
    const sink1 = makeSink();
    dumpStartupProfile(sink1.stream);
    const first = sink1.output();
    expect(first.length).toBeGreaterThan(0);

    const sink2 = makeSink();
    dumpStartupProfile(sink2.stream);
    expect(sink2.output()).toBe("");
  });

  it("emits nothing when enabled but no phases were marked", () => {
    process.env.REASONIX_PROFILE_STARTUP = "1";
    const sink = makeSink();
    dumpStartupProfile(sink.stream);
    expect(sink.output()).toBe("");
  });

  it("each line shows ms + phase name + (+delta) suffix", () => {
    process.env.REASONIX_PROFILE_STARTUP = "1";
    markPhase("alpha");
    markPhase("beta");
    const sink = makeSink();
    dumpStartupProfile(sink.stream);
    const lines = sink.output().split("\n");
    const alphaLine = lines.find((l) => l.includes("alpha"));
    const betaLine = lines.find((l) => l.includes("beta"));
    expect(alphaLine).toMatch(/\d+ms\s+alpha\s+\(\+\d+\)/);
    expect(betaLine).toMatch(/\d+ms\s+beta\s+\(\+\d+\)/);
  });
});

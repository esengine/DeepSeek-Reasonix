import { describe, expect, it } from "vitest";
import {
  controllerIdFor,
  enrollProofMessage,
  importControllerKey,
  thumbprintOf,
  verifyProof,
} from "./controllerProof";
import { ENROLL_PROOF_VECTOR as v } from "./controllerProof.vectors";

const hex = (bytes: Uint8Array) => [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");

describe("controller proof encoding", () => {
  it("derives the RFC 7638 thumbprint from the canonical JWK", async () => {
    await expect(thumbprintOf(v.jwk)).resolves.toBe(v.thumbprint);
  });

  it("derives the enrollment id from the host and the thumbprint", async () => {
    await expect(controllerIdFor(v.hostDeviceId, v.thumbprint)).resolves.toBe(v.controllerId);
    await expect(controllerIdFor("cd".repeat(32), v.thumbprint)).resolves.not.toBe(v.controllerId);
  });

  it("encodes the label then length-prefixed fields in a fixed order", () => {
    const message = enrollProofMessage({
      nonce: v.nonce, userId: v.userId, hostDeviceId: v.hostDeviceId, thumbprint: v.thumbprint,
    });
    expect(hex(message)).toBe(v.messageHex);
  });

  it("changes the message when any signed field changes", () => {
    const base = { nonce: v.nonce, userId: v.userId, hostDeviceId: v.hostDeviceId, thumbprint: v.thumbprint };
    const reference = hex(enrollProofMessage(base));
    for (const changed of [
      { ...base, nonce: "ce".repeat(32) },
      { ...base, userId: 8 },
      { ...base, hostDeviceId: "ac".repeat(32) },
      { ...base, thumbprint: "A".repeat(43) },
    ]) {
      expect(hex(enrollProofMessage(changed))).not.toBe(reference);
    }
  });

  it("verifies a P1363 signature and rejects every other shape", async () => {
    const key = await importControllerKey(v.jwk);
    expect(key).not.toBeNull();
    const message = enrollProofMessage({
      nonce: v.nonce, userId: v.userId, hostDeviceId: v.hostDeviceId, thumbprint: v.thumbprint,
    });
    expect(await verifyProof(key!, message, v.signature)).toBe(true);
    const flipped = v.signature.slice(0, -2) + (v.signature.endsWith("A") ? "B" : "A") + v.signature.slice(-1);
    expect(await verifyProof(key!, message, flipped)).toBe(false);
    expect(await verifyProof(key!, message, v.signature.slice(0, 60))).toBe(false);
    expect(await verifyProof(key!, new Uint8Array([1]), v.signature)).toBe(false);
  });

  it("refuses a point that is not on the curve", async () => {
    expect(await importControllerKey({ ...v.jwk, y: v.jwk.x })).toBeNull();
  });

  it("refuses a non-canonical encoding of the same key", async () => {
    const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    const alias = (text: string) => text.slice(0, -1) + alphabet[alphabet.indexOf(text.slice(-1)) ^ 1];
    expect(await importControllerKey(v.jwk)).not.toBeNull();
    expect(await importControllerKey({ ...v.jwk, x: alias(v.jwk.x) })).toBeNull();
    expect(await importControllerKey({ ...v.jwk, y: alias(v.jwk.y) })).toBeNull();
  });
});

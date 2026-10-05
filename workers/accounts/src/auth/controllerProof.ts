// Proof of possession for a controller device key (ECDSA P-256 over SHA-256).
// A signature is WebCrypto's IEEE P1363 form: r and s, 32 bytes each.
const encoder = new TextEncoder();

export const CONTROLLER_PROOF_LABEL = "reasonix-controller-v1";
export const CONTROLLER_ID_LABEL = "reasonix-rc-v1";

export interface ControllerJwk {
  kty: "EC";
  crv: "P-256";
  x: string;
  y: string;
}

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64url(text: string): Uint8Array | null {
  if (!/^[A-Za-z0-9_-]*$/.test(text)) return null;
  const padded = text.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(text.length / 4) * 4, "=");
  try {
    const binary = atob(padded);
    return Uint8Array.from(binary, (char) => char.charCodeAt(0));
  } catch {
    return null;
  }
}

async function sha256(input: string): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", encoder.encode(input)));
}

// SHA-256 of the RFC 7638 canonical JWK: members crv, kty, x, y in that order.
export async function thumbprintOf(jwk: ControllerJwk): Promise<string> {
  const canonical = `{"crv":"${jwk.crv}","kty":"${jwk.kty}","x":"${jwk.x}","y":"${jwk.y}"}`;
  return base64url(await sha256(canonical));
}

// Self-certifying: the id names exactly one key on one host. Both inputs have
// a fixed length, so the concatenation is unambiguous.
export async function controllerIdFor(hostDeviceId: string, thumbprint: string): Promise<string> {
  return `rc_${base64url(await sha256(`${CONTROLLER_ID_LABEL}${hostDeviceId}${thumbprint}`))}`;
}

function lengthPrefixed(parts: string[]): Uint8Array {
  const chunks = [encoder.encode(CONTROLLER_PROOF_LABEL)];
  for (const part of parts) {
    const bytes = encoder.encode(part);
    chunks.push(new Uint8Array([bytes.length >> 8, bytes.length & 0xff]), bytes);
  }
  const out = new Uint8Array(chunks.reduce((total, chunk) => total + chunk.length, 0));
  let at = 0;
  for (const chunk of chunks) {
    out.set(chunk, at);
    at += chunk.length;
  }
  return out;
}

export function enrollProofMessage(input: {
  nonce: string;
  userId: number;
  hostDeviceId: string;
  thumbprint: string;
}): Uint8Array {
  return lengthPrefixed(["enroll", input.nonce, String(input.userId), input.hostDeviceId, input.thumbprint]);
}

// WebCrypto validates that the point lies on the curve; null means it does not.
// It also accepts base64url with nonzero spare bits, so the key is re-exported
// and the submitted coordinates must match the canonical encoding exactly:
// the key, not the string, decides identity.
export async function importControllerKey(jwk: ControllerJwk): Promise<CryptoKey | null> {
  try {
    const key = await crypto.subtle.importKey("jwk", jwk, { name: "ECDSA", namedCurve: "P-256" }, true, ["verify"]);
    const canonical = (await crypto.subtle.exportKey("jwk", key)) as JsonWebKey;
    return canonical.x === jwk.x && canonical.y === jwk.y ? key : null;
  } catch {
    return null;
  }
}

export async function verifyProof(key: CryptoKey, message: Uint8Array, signature: string): Promise<boolean> {
  const raw = fromBase64url(signature);
  if (!raw || raw.length !== 64) return false;
  try {
    return await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, key, raw, message);
  } catch {
    return false;
  }
}

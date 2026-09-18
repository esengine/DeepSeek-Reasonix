import { RpcError } from "../rpc.js";

// Codes the Go BrowserExecutor maps onto its kernel sentinels; anything else
// is a transport failure and therefore an unknown outcome for a reserved write.
export const BROWSER_ERR_STALE_REFERENCE = -32010;
export const BROWSER_ERR_TAKEN_OVER = -32011;
export const BROWSER_ERR_NO_GRANT = -32012;
export const BROWSER_ERR_INVALID_URL = -32013;
export const BROWSER_ERR_UNSUPPORTED_SCHEME = -32014;
export const BROWSER_ERR_TAB_UNAVAILABLE = -32015;
export const BROWSER_ERR_INVALID_ARGUMENTS = -32016;

export function staleReference(detail: string): RpcError {
  return new RpcError(BROWSER_ERR_STALE_REFERENCE, `stale reference: ${detail}`);
}

export function takenOver(detail: string): RpcError {
  return new RpcError(BROWSER_ERR_TAKEN_OVER, `tab taken over by the user: ${detail}`);
}

export function noGrant(detail: string): RpcError {
  return new RpcError(BROWSER_ERR_NO_GRANT, `no browser grant: ${detail}`);
}


export type BrowserRefusalKind = "invalid_url" | "unsupported_scheme" | "tab_unavailable" | "invalid_arguments";

const refusalCodes: Record<BrowserRefusalKind, number> = {
  invalid_url: BROWSER_ERR_INVALID_URL,
  unsupported_scheme: BROWSER_ERR_UNSUPPORTED_SCHEME,
  tab_unavailable: BROWSER_ERR_TAB_UNAVAILABLE,
  invalid_arguments: BROWSER_ERR_INVALID_ARGUMENTS,
};

export function browserRefusal(kind: BrowserRefusalKind, detail: string): RpcError {
  return new RpcError(refusalCodes[kind], detail, { kind, execution: "not_executed" });
}

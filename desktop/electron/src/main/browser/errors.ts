import { RpcError } from "../rpc.js";

// Codes the Go BrowserExecutor maps onto its kernel sentinels; anything else
// is a transport failure and therefore an unknown outcome for a reserved write.
export const BROWSER_ERR_STALE_REFERENCE = -32010;
export const BROWSER_ERR_TAKEN_OVER = -32011;
export const BROWSER_ERR_NO_GRANT = -32012;

export function staleReference(detail: string): RpcError {
  return new RpcError(BROWSER_ERR_STALE_REFERENCE, `stale reference: ${detail}`);
}

export function takenOver(detail: string): RpcError {
  return new RpcError(BROWSER_ERR_TAKEN_OVER, `tab taken over by the user: ${detail}`);
}

export function noGrant(detail: string): RpcError {
  return new RpcError(BROWSER_ERR_NO_GRANT, `no browser grant: ${detail}`);
}

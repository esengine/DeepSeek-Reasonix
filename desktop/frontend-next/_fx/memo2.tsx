import { mutation } from "./sink";

// M3 — the same shape as M2 with a local function of that name. This file
// imports nothing from react, so `memo` here is its own declaration: a wrapper
// is React's because it was imported from react, and a spelling confers
// nothing. `Shadowed` names no body this pass may resolve, so ShadowBody keeps
// having no render site and its prop stays unresolved.
function memo<T>(_x: T) {
  return ShadowBody;
}
function ShadowBody({ onGo }: { onGo: () => void }) {
  return <button onClick={() => onGo()}>m3</button>;
}
const Shadowed = memo(ShadowBody);
export function M3() {
  return <Shadowed onGo={mutation} />;
}

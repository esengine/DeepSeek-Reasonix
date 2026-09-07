import { Switch } from "./ui/Switch";
import { mutation } from "./sink";

// Two usages of one generic control, declared as two actions. The action a
// usage belongs to lives at the render site, and so does what it does: the
// writing one must not make the read-only one write. If membership ever
// propagates from a component's declaration instead of from its call site,
// this is the pair that fails.
export function P1() {
  return <Switch data-action="fx.writes" onClick={() => mutation()} />;
}
export function P2() {
  return <Switch data-action="fx.reads" onClick={() => undefined} />;
}

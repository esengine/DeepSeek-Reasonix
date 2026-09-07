// The fixture's own helper: the census must recognise it by where it is
// imported from, not by the product's path or by the callee's spelling.
export interface ActionListener {
  action: string;
  listener: EventListenerOrEventListenerObject;
  value?: string;
}
export function listenAction(target: EventTarget, type: string, spec: ActionListener): () => void {
  target.addEventListener(type, spec.listener);
  return () => target.removeEventListener(type, spec.listener);
}

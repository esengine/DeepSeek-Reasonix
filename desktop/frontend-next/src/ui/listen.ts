// Where a DOM listener says which action it is.
//
// A button carries its identity as an attribute and the shortcut table carries
// it as a field; a listener had nowhere to put one, so every keyboard entry
// point in Studio was outside the action vocabulary — including two that are
// proven to change kernel state.
//
// The declaration belongs to the registration, not to the callback: one
// function can be registered on several targets and events, and those are
// different entry points that may one day be different intents. Tagging the
// callback would merge them before anyone decided to.
//
// It says which action this input belongs to and nothing else. Whether the
// registration is a user input at all is still decided by what the target is
// and what the event means — an `action` on a media query or on a resize is an
// error, not a promotion.

export interface ActionListener {
  /** The same vocabulary data-action uses: <surface>.<intent>, a literal. */
  action: string;
  listener: EventListenerOrEventListenerObject;
  /** Which entity or answer this input is about, when the action has one. */
  value?: string;
}

/** addEventListener, with the action this registration belongs to written down.
 *  Returns the disposer, so an effect can return it directly. */
export function listenAction(
  target: EventTarget,
  type: string,
  spec: ActionListener,
  options?: AddEventListenerOptions | boolean,
): () => void {
  target.addEventListener(type, spec.listener, options);
  return () => target.removeEventListener(type, spec.listener, options);
}

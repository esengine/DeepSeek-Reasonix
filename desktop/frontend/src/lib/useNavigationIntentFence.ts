import { useCallback, useRef } from "react";
import { app } from "./bridge";

interface NavigationIntentRegistration {
  token: string;
  registered: Promise<string>;
}

let navigationIntentRegistrationTail: Promise<void> = Promise.resolve();
let navigationIntentCounter = 0;

function navigationIntentToken(hint: string): string {
  const nonce = new Uint32Array(4);
  globalThis.crypto.getRandomValues(nonce);
  return `nav-${hint}-${(++navigationIntentCounter).toString(36)}-${Array.from(nonce, (value) => value.toString(36)).join("-")}`;
}

function scheduleNavigationIntent(hint: string): NavigationIntentRegistration {
  const token = navigationIntentToken(hint);
  const binding = app.RegisterNavigationIntent;
  const registered = navigationIntentRegistrationTail.then(async () => {
    if (typeof binding !== "function") throw new Error("navigation intent binding is unavailable");
    await binding.call(app, token);
    return token;
  });
  navigationIntentRegistrationTail = registered.then(() => undefined, () => undefined);
  return { token, registered };
}

export function publishNavigationIntent(hint = "direct"): Promise<string> {
  return scheduleNavigationIntent(hint).registered;
}

export function useNavigationIntentFence() {
  const registrationsRef = useRef(new Map<number, NavigationIntentRegistration>());
  const registerNavigationIntent = useCallback((seq: number) => {
    const scheduled = scheduleNavigationIntent(seq.toString(36));
    const registration: NavigationIntentRegistration = {
      token: scheduled.token,
      registered: scheduled.registered.catch(() => ""),
    };
    registrationsRef.current.clear();
    registrationsRef.current.set(seq, registration);
  }, []);
  const registeredNavigationIntent = useCallback(async (seq: number): Promise<string> => {
    const registration = registrationsRef.current.get(seq);
    if (!registration) return "";
    const token = await registration.registered;
    if (registrationsRef.current.get(seq)?.token !== token) return "";
    return token;
  }, []);
  return { registerNavigationIntent, registeredNavigationIntent };
}

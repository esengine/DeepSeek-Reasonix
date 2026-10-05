// How the kernel reaches the outside, and what probing that found.
// Proxy settings as the editor needs them. The stored password never comes back
// out — hasPassword only says one exists, so the form can keep or clear it.
export interface NetworkSettings {
  mode: string;
  url?: string;
  noProxy?: string;
  type?: string;
  server?: string;
  port?: number;
  username?: string;
  hasPassword?: boolean;
  effective: string;
  direct?: string[];
  endpoint?: string;
}

// One diagnosed step. advice is the next thing to try, present only when the
// cause is knowable from the failure.
export interface NetworkProbe {
  step: string;
  ok: boolean;
  detail: string;
  durationMs: number;
  advice?: string;
}

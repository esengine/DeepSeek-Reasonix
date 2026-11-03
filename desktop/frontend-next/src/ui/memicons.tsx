import type { ReactNode } from "react";

const LINE: Record<string, ReactNode> = {
  all: <><rect x="2.5" y="2.5" width="4.5" height="4.5" rx="1" /><rect x="9" y="2.5" width="4.5" height="4.5" rx="1" /><rect x="2.5" y="9" width="4.5" height="4.5" rx="1" /><rect x="9" y="9" width="4.5" height="4.5" rx="1" /></>,
  new: <path d="M8 2.5l1.3 3.2 3.2 1.3-3.2 1.3L8 11.5 6.7 8.3 3.5 7l3.2-1.3z" />,
  constraint: <><rect x="3.5" y="7" width="9" height="6.5" rx="1.5" /><path d="M5.5 7V5.5a2.5 2.5 0 015 0V7" /></>,
  user: <><circle cx="8" cy="5.5" r="2.6" /><path d="M3 13.5c.7-2.6 2.6-3.8 5-3.8s4.3 1.2 5 3.8" /></>,
  feedback: <path d="M2.5 3.5h11v7h-6l-3 2.5v-2.5h-2z" />,
  project: <path d="M2 4.5h4l1.5 1.5H14v6.5H2z" />,
  reference: <path d="M6.5 9.5l3-3M5 8l-1.2 1.2a2.2 2.2 0 003.1 3.1L8 11M11 8l1.2-1.2a2.2 2.2 0 00-3.1-3.1L8 5" />,
  galaxy: <path d="M8 8c0-1 1.5-1 1.5.2s-1.2 2.5-2.8 2.2S4 8 4.6 5.8 7.4 3 9.8 3.8s3.7 3 3 5.4" />,
  archive: <><rect x="2.5" y="3.5" width="11" height="3" rx=".8" /><path d="M3.5 6.5v6h9v-6M6.5 9h3" /></>,
  search: <><circle cx="7" cy="7" r="4" /><path d="M10 10l3.5 3.5" /></>,
  importance: <path d="M4 12V9M8 12V6M12 12V3" />,
  clock: <><circle cx="8" cy="8" r="5.5" /><path d="M8 5v3.2l2 1.3" /></>,
  uses: <path d="M3 7a4 4 0 014-4h5l-1.5-1.5M13 9a4 4 0 01-4 4H4l1.5 1.5" />,
  check: <path d="M3.5 8.5l3 3 6-6.5" />,
  edit: <path d="M3 13l.8-3L11 2.8a1.4 1.4 0 012 2L5.8 12z" />,
  cross: <path d="M4 4l8 8M12 4l-8 8" />,
  pin: <path d="M6 2.5h4l-.5 4 2 2H4.5l2-2zM8 8.5v5" />,
};

export function Icon({ name, size = 16 }: { name: string; size?: number }) {
  return (
    <svg className="memi" width={size} height={size} style={{ width: size, height: size, display: "block", flex: "none" }} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.45"
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {LINE[name] ?? LINE.project}
    </svg>
  );
}

export function StateIcon({ state, size = 16 }: { state?: string; size?: number }) {
  return (
    <svg className="memi" width={size} height={size} style={{ width: size, height: size, display: "block", flex: "none" }} viewBox="0 0 16 16" aria-hidden="true" data-state={state ?? "clear"}>
      {state === "faded" ? (
        <circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeDasharray="2 2.2" />
      ) : (
        <>
          <circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" strokeWidth="1.4" />
          {state === "fuzzy" ? <path d="M8 2.5A5.5 5.5 0 0 1 8 13.5Z" fill="currentColor" /> : <circle cx="8" cy="8" r="5.5" fill="currentColor" />}
        </>
      )}
    </svg>
  );
}

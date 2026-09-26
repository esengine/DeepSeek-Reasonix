import type { SVGProps } from "react";

// Exact outline geometry used by the Reasonix Studio prototype.
const PATHS = {
  brand: "M6 20V4h7a5 5 0 0 1 0 10H6m7 0 6 6",
  folder: "M3 7V5h6l3 3h9v12H3V7z",
  reveal: "M12 20H3V5h6l3 3h9v4M15 20l6-6M16 14h5v5",
  file: "M14 3H5v18h14V8l-5-5v5h5M8 12h8M8 16h5",
  search: "M15 15l6 6M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0",
  chevron: "m9 5 7 7-7 7",
  down: "m6 9 6 6 6-6",
  plus: "M12 5v14M5 12h14",
  arrow: "M12 20V4m-6 6 6-6 6 6",
  close: "m6 6 12 12M6 18 18 6",
  check: "m5 12 4 4L19 6",
  globe: "M3 12h18M12 3c-5 5-5 13 0 18 5-5 5-13 0-18M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0",
  code: "m8 6-6 6 6 6m8-12 6 6-6 6M14 3l-4 18",
  settings: "M4 7h16M4 17h16M9 4v6M16 14v6",
  sliders: "M4 6h16M4 12h16M4 18h16M8 3v6M16 9v6M10 15v6",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  menu: "M4 6h16M4 12h16M4 18h16",
  panel: "M4 5h16v14H4V5zM9 5v14",
  shield: "m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3zM8 12l3 3 5-6",
  branch: "M6 5a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm0 0v12m12-8a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm0 0a6 6 0 0 1-6 6H6m0 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z",
  layers: "m12 3 10 5-10 5L2 8l10-5zM2 12l10 5 10-5M2 16l10 5 10-5",
  spark: "m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3z",
  agent: "M11 3l1.7 4.3L17 9l-4.3 1.7L11 15l-1.7-4.3L5 9l4.3-1.7L11 3zM19 14l.8 2.2L22 17l-2.2.8L19 20l-.8-2.2L16 17l2.2-.8L19 14zM4 3l.5 1.5L6 5l-1.5.5L4 7l-.5-1.5L2 5l1.5-.5L4 3z",
  refresh: "M20 7a9 9 0 1 0 1 8M20 3v5h-5",
  clock: "M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0",
  stop: "M6 6h12v12H6z",
  play: "m8 4 12 8-12 8V4z",
  pause: "M8 5v14M16 5v14",
  warning: "M12 3 2 21h20L12 3zM12 9v5M12 17h.01",
  copy: "M8 8h13v13H8V8zM16 8V3H3v13h5",
  quote: "M7 7H3v6h6V9m0-2v8a2 2 0 0 1-2 2H5m16-10h-4v6h6V9m0-2v8a2 2 0 0 1-2 2h-2",
  external: "M14 3h7v7M21 3 11 13M10 3H3v18h18v-7",
  plug: "M8 3v5m8-5v5M5 8h14v4a7 7 0 0 1-14 0V8M12 19v3",
  download: "M12 3v12m-5-5 5 5 5-5M4 17v4h16v-4",
  list: "M9 5h12M9 12h12M9 19h12M3 5h.01M3 12h.01M3 19h.01",
  edit: "m4 16 12-12 4 4L8 20H4v-4zM13 7l4 4",
  pin: "M9 3h6l-1 6 4 4H6l4-4-1-6zM12 13v8",
  trash: "M4 7h16M9 7V4h6v3M7 7l1 14h8l1-14M10 11v6M14 11v6",
  archive: "M4 4h16v5H4V4zm2 5v11h12V9M9 13h6",
  server: "M3 3h18v7H3V3zM3 14h18v7H3v-7zM7 6h.01M7 17h.01",
  link: "m10 13 4-4M9 16l-2 2a4 4 0 0 1-6-5l4-4a4 4 0 0 1 6 0M14 8l2-2a4 4 0 0 1 6 5l-4 4a4 4 0 0 1-6 0",
  wallet: "M3 6h17v15H3V6zM3 6l14-4v4M15 11h7v6h-7v-6zM18 14h.01",
  gauge: "M4 18a8 8 0 1 1 16 0M12 18l4-6M7 17h.01M17 17h.01M12 9v.01",
} as const;

export type StudioIconName = keyof typeof PATHS;

export function StudioIcon({ name, className, ...props }: SVGProps<SVGSVGElement> & { name: StudioIconName }) {
  return (
    <svg
      className={["studio-icon", className].filter(Boolean).join(" ")}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      <path d={PATHS[name]} />
    </svg>
  );
}

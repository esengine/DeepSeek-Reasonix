import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function Ic({ size = 14, children, ...rest }: IconProps & { children: React.ReactNode }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      {...rest}
    >
      {children}
    </svg>
  );
}

export const I = {
  plus: (p: IconProps) => (<Ic {...p}><path d="M12 5v14M5 12h14" /></Ic>),
  search: (p: IconProps) => (<Ic {...p}><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></Ic>),
  send: (p: IconProps) => (<Ic {...p}><path d="M4 12 20 4l-6 16-3-7-7-1Z" /></Ic>),
  chev: (p: IconProps) => (<Ic {...p}><path d="m6 9 6 6 6-6" /></Ic>),
  chevR: (p: IconProps) => (<Ic {...p}><path d="m9 6 6 6-6 6" /></Ic>),
  check: (p: IconProps) => (<Ic {...p}><path d="m5 12 5 5L20 7" /></Ic>),
  x: (p: IconProps) => (<Ic {...p}><path d="M6 6l12 12M18 6 6 18" /></Ic>),
  terminal: (p: IconProps) => (<Ic {...p}><rect x="3" y="4" width="18" height="16" rx="2" /><path d="m7 9 3 3-3 3M13 15h4" /></Ic>),
  brain: (p: IconProps) => (
    <Ic {...p}>
      <path d="M9 4a3 3 0 0 0-3 3v0a3 3 0 0 0-2 5 3 3 0 0 0 2 5 3 3 0 0 0 3 3h0a3 3 0 0 0 3-3V4" />
      <path d="M15 4a3 3 0 0 1 3 3 3 3 0 0 1 2 5 3 3 0 0 1-2 5 3 3 0 0 1-3 3" />
    </Ic>
  ),
  list: (p: IconProps) => (<Ic {...p}><path d="M4 6h2M4 12h2M4 18h2M9 6h11M9 12h11M9 18h11" /></Ic>),
  diff: (p: IconProps) => (<Ic {...p}><path d="M7 4v11a3 3 0 0 0 3 3h2M17 20V9a3 3 0 0 0-3-3h-2" /><circle cx="7" cy="20" r="2" /><circle cx="17" cy="4" r="2" /></Ic>),
  globe: (p: IconProps) => (<Ic {...p}><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" /></Ic>),
  link: (p: IconProps) => (<Ic {...p}><path d="M10 14a4 4 0 0 1 0-6l3-3a4 4 0 0 1 6 6l-2 2" /><path d="M14 10a4 4 0 0 1 0 6l-3 3a4 4 0 0 1-6-6l2-2" /></Ic>),
  wrench: (p: IconProps) => (<Ic {...p}><path d="M14 7a4 4 0 1 0 4 4l3 3-3 3-3-3a4 4 0 0 1-4-4l-3-3-3 3 3 3a4 4 0 0 0 6 0" /></Ic>),
  bot: (p: IconProps) => (<Ic {...p}><rect x="4" y="7" width="16" height="12" rx="3" /><path d="M12 3v4M8 12h.01M16 12h.01M9 16h6" /></Ic>),
  archive: (p: IconProps) => (<Ic {...p}><rect x="3" y="3" width="18" height="5" rx="1" /><path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8M10 12h4" /></Ic>),
  bookmark: (p: IconProps) => (<Ic {...p}><path d="M6 3h12v18l-6-4-6 4Z" /></Ic>),
  warning: (p: IconProps) => (<Ic {...p}><path d="M12 3 2 21h20Z" /><path d="M12 10v5M12 18h.01" /></Ic>),
  zap: (p: IconProps) => (<Ic {...p}><path d="M13 3 4 14h7l-1 7 9-11h-7Z" /></Ic>),
  database: (p: IconProps) => (<Ic {...p}><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6" /></Ic>),
  cpu: (p: IconProps) => (<Ic {...p}><rect x="5" y="5" width="14" height="14" rx="2" /><rect x="9" y="9" width="6" height="6" /><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3" /></Ic>),
  coin: (p: IconProps) => (<Ic {...p}><circle cx="12" cy="12" r="9" /><path d="M12 7v10M9 10h4a2 2 0 0 1 0 4H9" /></Ic>),
  file: (p: IconProps) => (<Ic {...p}><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Z" /><path d="M14 3v6h6" /></Ic>),
  folder: (p: IconProps) => (<Ic {...p}><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /></Ic>),
  image: (p: IconProps) => (<Ic {...p}><rect x="3" y="4" width="18" height="16" rx="2" /><circle cx="9" cy="10" r="2" /><path d="m3 18 5-5 4 4 3-3 6 6" /></Ic>),
  paperclip: (p: IconProps) => (<Ic {...p}><path d="M21 12 12 21a5 5 0 0 1-7-7L14 5a3 3 0 0 1 4 4l-9 9a1 1 0 0 1-2-2l8-8" /></Ic>),
  mic: (p: IconProps) => (<Ic {...p}><rect x="9" y="3" width="6" height="12" rx="3" /><path d="M5 11a7 7 0 0 0 14 0M12 18v3M9 21h6" /></Ic>),
  sun: (p: IconProps) => (<Ic {...p}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5" /></Ic>),
  moon: (p: IconProps) => (<Ic {...p}><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" /></Ic>),
  panel_l: (p: IconProps) => (<Ic {...p}><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M9 4v16" /></Ic>),
  panel_r: (p: IconProps) => (<Ic {...p}><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M15 4v16" /></Ic>),
  cog: (p: IconProps) => (<Ic {...p}><circle cx="12" cy="12" r="3" /><path d="M12 2v3M12 19v3M4.2 4.2l2.1 2.1M17.7 17.7l2.1 2.1M2 12h3M19 12h3M4.2 19.8l2.1-2.1M17.7 6.3l2.1-2.1" /></Ic>),
  stop: (p: IconProps) => (<Ic {...p}><rect x="6" y="6" width="12" height="12" rx="2" /></Ic>),
  play: (p: IconProps) => (<Ic {...p}><path d="M7 5v14l12-7Z" /></Ic>),
  more: (p: IconProps) => (<Ic {...p}><circle cx="6" cy="12" r="1.6" fill="currentColor" /><circle cx="12" cy="12" r="1.6" fill="currentColor" /><circle cx="18" cy="12" r="1.6" fill="currentColor" /></Ic>),
  pin: (p: IconProps) => (<Ic {...p}><path d="M15 3 9 9l-4 1 9 9 1-4 6-6ZM4 20l5-5" /></Ic>),
  rotate: (p: IconProps) => (<Ic {...p}><path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5" /></Ic>),
  branch: (p: IconProps) => (<Ic {...p}><circle cx="6" cy="4" r="2" /><circle cx="6" cy="20" r="2" /><circle cx="18" cy="8" r="2" /><path d="M6 6v12M6 14a6 6 0 0 0 6-6h4" /></Ic>),
  at: (p: IconProps) => (<Ic {...p}><circle cx="12" cy="12" r="4" /><path d="M16 8v6a3 3 0 0 0 5-2 9 9 0 1 0-4 7" /></Ic>),
  slash: (p: IconProps) => (<Ic {...p}><path d="M16 4 8 20" /></Ic>),
  layers: (p: IconProps) => (<Ic {...p}><path d="m12 3 9 5-9 5-9-5Z" /><path d="m3 13 9 5 9-5M3 18l9 5 9-5" /></Ic>),
  download: (p: IconProps) => (<Ic {...p}><path d="M12 4v12M6 12l6 6 6-6M4 20h16" /></Ic>),
  upload: (p: IconProps) => (<Ic {...p}><path d="M12 20V8M6 12l6-6 6 6M4 4h16" /></Ic>),
  history: (p: IconProps) => (<Ic {...p}><path d="M3 12a9 9 0 1 0 3-6.7L3 8M12 7v5l3 2" /></Ic>),
  shield: (p: IconProps) => (<Ic {...p}><path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6Z" /><path d="m9 12 2 2 4-4" /></Ic>),
  warn: (p: IconProps) => (<Ic {...p}><path d="M12 3 2 21h20Z" /><path d="M12 10v5M12 18h.01" /></Ic>),
  help: (p: IconProps) => (<Ic {...p}><circle cx="12" cy="12" r="9" /><path d="M9.5 9a2.5 2.5 0 0 1 5 0c0 1.5-2.5 2-2.5 4M12 17h.01" /></Ic>),
  refresh: (p: IconProps) => (<Ic {...p}><path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 4v4h-4M21 12a9 9 0 0 1-15 6.7L3 16M3 20v-4h4" /></Ic>),
};

export type IconKey = keyof typeof I;

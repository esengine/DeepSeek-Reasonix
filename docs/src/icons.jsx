// SVG icon components, minimal stroke style

const Ic = {
  Github: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="currentColor" {...p}>
      <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-2c-3.2.69-3.87-1.54-3.87-1.54-.52-1.33-1.27-1.69-1.27-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.76 2.69 1.25 3.34.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.27-5.24-5.66 0-1.25.45-2.27 1.18-3.07-.12-.29-.51-1.46.11-3.04 0 0 .97-.31 3.18 1.17.92-.26 1.91-.39 2.89-.39.98 0 1.97.13 2.89.39 2.21-1.48 3.18-1.17 3.18-1.17.62 1.58.23 2.75.11 3.04.73.8 1.18 1.82 1.18 3.07 0 4.4-2.69 5.36-5.25 5.65.41.36.78 1.06.78 2.14v3.17c0 .31.21.68.8.56C20.21 21.39 23.5 17.07 23.5 12 23.5 5.65 18.35.5 12 .5z"/>
    </svg>
  ),
  Terminal: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <rect x="2.5" y="4" width="19" height="16" rx="2"/>
      <path d="M6 9l3 3-3 3M12 15h6"/>
    </svg>
  ),
  Download: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M12 3v13M6 11l6 6 6-6M4 21h16"/>
    </svg>
  ),
  Copy: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||14} height={p.size||14} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <rect x="8" y="8" width="13" height="13" rx="2"/>
      <path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>
    </svg>
  ),
  Check: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||14} height={p.size||14} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M20 6L9 17l-5-5"/>
    </svg>
  ),
  Chev: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M6 9l6 6 6-6"/>
    </svg>
  ),
  Arrow: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||14} height={p.size||14} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M5 12h14M13 5l7 7-7 7"/>
    </svg>
  ),
  Bolt: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M13 2L4 14h7l-1 8 9-12h-7l1-8z"/>
    </svg>
  ),
  Shield: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
    </svg>
  ),
  Cube: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M12 2l9 5v10l-9 5-9-5V7l9-5zM3 7l9 5 9-5M12 12v10"/>
    </svg>
  ),
  Plug: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M9 7V2M15 7V2M5 12h14v2a6 6 0 0 1-6 6h-2a6 6 0 0 1-6-6v-2zM12 20v2"/>
    </svg>
  ),
  Brain: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M9 4a3 3 0 0 0-3 3v1a3 3 0 0 0-2 2.83V13a3 3 0 0 0 2 2.83V17a3 3 0 0 0 6 0V4a3 3 0 0 0-3 0zM15 4a3 3 0 0 1 3 3v1a3 3 0 0 1 2 2.83V13a3 3 0 0 1-2 2.83V17a3 3 0 0 1-6 0"/>
    </svg>
  ),
  Plugin: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M14 7V3h-4v4H6v4a4 4 0 0 0 4 4h4a4 4 0 0 0 4-4V7h-4zM12 15v6"/>
    </svg>
  ),
  Eye: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||18} height={p.size||18} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8S1 12 1 12z"/><circle cx="12" cy="12" r="3"/>
    </svg>
  ),
  Gitee: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="currentColor" {...p}>
      <path d="M12 .5C5.65.5.5 5.65.5 12S5.65 23.5 12 23.5 23.5 18.35 23.5 12 18.35.5 12 .5zm5.91 9.62v1.45c0 .76-.62 1.38-1.38 1.38h-5.05v1.45h7.21c-.04 2.6-2.16 4.7-4.77 4.7H9.6a1.38 1.38 0 0 1-1.38-1.38v-5.05c0-2.62 2.13-4.75 4.75-4.75h4.94z"/>
    </svg>
  ),
  Cloud: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" {...p}>
      <path d="M17.5 19a4.5 4.5 0 0 0 .25-9 6 6 0 0 0-11.5 1.5A4 4 0 0 0 6 19h11.5z"/>
    </svg>
  ),
  Star: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||14} height={p.size||14} fill="currentColor" {...p}>
      <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
    </svg>
  ),
  Sparkle: (p) => (
    <svg viewBox="0 0 24 24" width={p.size||16} height={p.size||16} fill="currentColor" {...p}>
      <path d="M12 2l1.5 5.5L19 9l-5.5 1.5L12 16l-1.5-5.5L5 9l5.5-1.5L12 2zM19 14l.8 2.7L22 17.5l-2.2.8L19 21l-.8-2.7L16 17.5l2.2-.8L19 14z"/>
    </svg>
  ),
};

window.Ic = Ic;

// Shared editorial section header
function SecHead({ num, label, title, sub, actions }) {
  return (
    <>
      <div className="sec-meta">
        <span className="sec-num">§{num}</span>
        <span>· {label}</span>
        <span className="rule"></span>
      </div>
      <div className="section-head">
        <div className="section-head-text">
          <h2 className="section-title" dangerouslySetInnerHTML={{__html: title}}/>
        </div>
        <div style={{display:'flex', flexDirection:'column', gap:18, alignItems:'flex-end'}}>
          {sub && <p className="section-sub">{sub}</p>}
          {actions && <div className="section-head-actions">{actions}</div>}
        </div>
      </div>
    </>
  );
}
window.SecHead = SecHead;

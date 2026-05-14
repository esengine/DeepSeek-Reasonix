// Smart mirror grid — probes R2 / GitHub Releases for fastest TTFB and offers
// a per-platform installer link from whichever wins.

const VERSION = "0.42.0-3";
const GH_REPO = "esengine/DeepSeek-Reasonix";
const R2_BASE = "https://pub-147fb53b9c1e4bbf891a257968619ea7.r2.dev";

const MIRRORS = [
  {
    id: "r2",
    name: "Cloudflare R2",
    region: "GLOBAL · CF Edge",
    icon: "Cloud",
    base: `${R2_BASE}/v${VERSION}`,
    probe: `${R2_BASE}/latest/latest.json`,
  },
  {
    id: "github",
    name: "GitHub Releases",
    region: "GLOBAL · US",
    icon: "Github",
    base: `https://github.com/${GH_REPO}/releases/download/v${VERSION}`,
    probe: `https://github.com/${GH_REPO}/releases/download/v${VERSION}/latest.json`,
  },
];

const OS_OPTIONS = [
  {
    id: "mac",
    label: "macOS",
    file: `Reasonix_${VERSION}_universal.dmg`,
    size: "53 MB",
    note: "Universal — Apple Silicon + Intel",
  },
  {
    id: "win",
    label: "Windows",
    file: `Reasonix_${VERSION}_x64-setup.exe`,
    size: "30 MB",
    note: "NSIS installer · x64",
  },
  {
    id: "linux",
    label: "Linux",
    file: `Reasonix_${VERSION}_amd64.AppImage`,
    size: "122 MB",
    note: "AppImage · x86_64",
  },
];

function MirrorIcon({ name }) {
  const Icon = Ic[name];
  return Icon ? <Icon size={16}/> : null;
}

function detectOS() {
  if (typeof navigator === "undefined") return "mac";
  const ua = navigator.userAgent || "";
  if (/Mac/i.test(ua)) return "mac";
  if (/Win/i.test(ua)) return "win";
  if (/Linux/i.test(ua)) return "linux";
  return "mac";
}

// HEAD-probe a small URL on each mirror and return TTFB in ms. Uses `no-cors`
// so cross-origin probes still complete (we can't read the response but we
// can time it). 5 s ceiling; failures return null.
async function probeMirror(url) {
  const t0 = performance.now();
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 5000);
    await fetch(url, { method: "GET", mode: "no-cors", cache: "no-store", signal: ctrl.signal });
    clearTimeout(timer);
    return Math.round(performance.now() - t0);
  } catch {
    return null;
  }
}

function MirrorGrid({ os, setOs }) {
  const [testing, setTesting] = React.useState(true);
  const [results, setResults] = React.useState(() =>
    MIRRORS.map(m => ({ id: m.id, lat: null, done: false, ok: false }))
  );

  const runTest = React.useCallback(() => {
    setTesting(true);
    setResults(MIRRORS.map(m => ({ id: m.id, lat: null, done: false, ok: false })));
    let remaining = MIRRORS.length;
    MIRRORS.forEach((m) => {
      probeMirror(m.probe).then((lat) => {
        setResults(prev => prev.map(r =>
          r.id === m.id ? { id: m.id, lat, done: true, ok: lat != null } : r
        ));
        remaining -= 1;
        if (remaining === 0) setTesting(false);
      });
    });
  }, []);

  React.useEffect(() => {
    const t = setTimeout(runTest, 200);
    return () => clearTimeout(t);
  }, [runTest]);

  const fastest = React.useMemo(() => {
    const done = results.filter(r => r.done && r.ok && r.lat != null);
    if (done.length === 0) return null;
    return done.reduce((a, b) => (a.lat <= b.lat ? a : b)).id;
  }, [results]);

  const currentOs = OS_OPTIONS.find(o => o.id === os) || OS_OPTIONS[0];
  const fastestMirror = MIRRORS.find(m => m.id === fastest);
  const downloadUrl = fastestMirror ? `${fastestMirror.base}/${currentOs.file}` : null;

  return (
    <div>
      <div className="dl-toolbar">
        <div className="tabs">
          {OS_OPTIONS.map(o => (
            <button key={o.id} className={os === o.id ? "on" : ""} onClick={() => setOs(o.id)}>
              {o.label}
            </button>
          ))}
        </div>
        <button
          className="btn btn-ghost btn-sm"
          onClick={runTest}
          disabled={testing}
          title="重新探测"
          style={{ marginLeft: "auto" }}
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" style={testing ? { animation: "spin 0.9s linear infinite" } : {}}>
            <path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5"/>
          </svg>
          {testing ? "探测中…" : "重新探测"}
        </button>
      </div>

      <div className="mirrors">
        {MIRRORS.map(m => {
          const r = results.find(x => x.id === m.id);
          const isFastest = m.id === fastest && r.done && r.ok;
          const isTesting = !r.done;
          const failed = r.done && !r.ok;
          const url = `${m.base}/${currentOs.file}`;
          return (
            <div key={m.id} className={"mirror " + (isFastest ? "fastest " : "") + (isTesting ? "testing" : "")}>
              <div className="mirror-head">
                <span className="mirror-icon"><MirrorIcon name={m.icon}/></span>
                <div>
                  <div className="mirror-name">{m.name}</div>
                  <div className="mirror-region">{m.region}</div>
                </div>
                <span className="mirror-badge">Fastest</span>
              </div>
              <div className="mirror-stats">
                <div className="mirror-stat">
                  <label>延迟 · TTFB</label>
                  <b>
                    {r.done ? (r.ok ? r.lat : "—") : "—"}
                    <span className="unit">{r.done && r.ok ? "ms" : ""}</span>
                  </b>
                </div>
                <div className="mirror-stat">
                  <label>状态 · Status</label>
                  <b>{isTesting ? "probing…" : failed ? "unreachable" : "online"}</b>
                </div>
              </div>
              <a
                href={url}
                className={"btn btn-sm " + (isFastest ? "btn-primary" : "btn-ghost")}
                style={{ marginTop: 18, width: "100%", justifyContent: "center", ...(failed ? { opacity: 0.5, pointerEvents: "none" } : {}) }}
              >
                从此镜像下载 →
              </a>
            </div>
          );
        })}
      </div>

      <div className="dl-summary">
        <div className="info">
          {testing ? (
            <>正在探测最快镜像 <span style={{ color: "var(--cream-mute)" }}>· measuring TTFB</span></>
          ) : fastestMirror ? (
            <>
              已为你选择 <b>{fastestMirror.name}</b>
              <span style={{ color: "var(--cream-mute)" }}> · {currentOs.file} · {currentOs.size}</span>
            </>
          ) : "探测失败 · 请手动选择镜像"}
        </div>
        <a
          href={downloadUrl || "#"}
          className="btn btn-primary"
          style={testing || !downloadUrl ? { opacity: 0.5, pointerEvents: "none" } : {}}
        >
          下载 {currentOs.label} 版本 →
        </a>
      </div>

      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );
}

window.MirrorGrid = MirrorGrid;
window.detectOS = detectOS;
window.REASONIX_VERSION = VERSION;

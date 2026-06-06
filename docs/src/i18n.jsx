// Lang context + t() helper. `t({zh, en})` returns the active-lang string.

// Version is pulled from the R2 mirror's `latest/latest.json` on every page
// load. Empty until the fetch resolves so consumers can render a loading
// state instead of a stale literal — pre-empts the "docs version is wrong"
// drift that hardcoding caused. Each release just overwrites latest.json.
window.REASONIX_VERSION = window.REASONIX_VERSION || "";
window.REASONIX_VERSION_STATUS = window.REASONIX_VERSION
  ? "ok"
  : "loading";

(function fetchVersion() {
  var url = "https://pub-147fb53b9c1e4bbf891a257968619ea7.r2.dev/latest/latest.json";
  var ctrl = new AbortController();
  var timer = setTimeout(function () {
    ctrl.abort();
  }, 5000);
  fetch(url, { signal: ctrl.signal, cache: "no-cache" })
    .then(function (r) {
      return r.ok ? r.json() : null;
    })
    .then(function (j) {
      clearTimeout(timer);
      if (j && j.version) {
        window.REASONIX_VERSION = String(j.version).replace(/^v/, "");
        window.REASONIX_VERSION_STATUS = "ok";
      } else {
        window.REASONIX_VERSION_STATUS = "failed";
      }
      window.dispatchEvent(new Event("reasonix:version"));
    })
    .catch(function () {
      clearTimeout(timer);
      window.REASONIX_VERSION_STATUS = "failed";
      window.dispatchEvent(new Event("reasonix:version"));
    });
})();

function useVersion() {
  const [v, setV] = React.useState({
    version: window.REASONIX_VERSION,
    status: window.REASONIX_VERSION_STATUS,
  });
  React.useEffect(() => {
    const handler = () =>
      setV({
        version: window.REASONIX_VERSION,
        status: window.REASONIX_VERSION_STATUS,
      });
    window.addEventListener("reasonix:version", handler);
    return () => window.removeEventListener("reasonix:version", handler);
  }, []);
  return v;
}

const LangCtx = React.createContext({ lang: "zh", setLang: () => {} });

function detectInitialLang() {
  try {
    const url = new URLSearchParams(location.search).get("lang");
    if (url === "en" || url === "zh") return url;
    const stored = localStorage.getItem("reasonix.lang");
    if (stored === "en" || stored === "zh") return stored;
    const nav = (navigator.language || "").toLowerCase();
    return nav.startsWith("zh") ? "zh" : "en";
  } catch {
    return "zh";
  }
}

function LangProvider({ children }) {
  const [lang, setLangState] = React.useState(detectInitialLang);
  const setLang = React.useCallback((v) => {
    setLangState(v);
    try {
      localStorage.setItem("reasonix.lang", v);
      const url = new URL(window.location.href);
      url.searchParams.set("lang", v);
      window.history.replaceState({}, "", url.toString());
    } catch {}
  }, []);
  React.useEffect(() => {
    document.documentElement.lang = lang === "en" ? "en" : "zh-CN";
  }, [lang]);
  const value = React.useMemo(() => ({ lang, setLang }), [lang, setLang]);
  return <LangCtx.Provider value={value}>{children}</LangCtx.Provider>;
}

function useLang() {
  return React.useContext(LangCtx);
}

// Resolve a bilingual string. Plain strings pass through unchanged.
function t(s, lang) {
  if (s == null) return "";
  if (typeof s === "string") return s;
  return s[lang] || s.zh || s.en || "";
}

window.LangCtx = LangCtx;
window.LangProvider = LangProvider;
window.useLang = useLang;
window.useVersion = useVersion;
window.t = t;

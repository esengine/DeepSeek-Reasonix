// Top nav — single bar shared between index and download pages

function Nav({ active }) {
  const { version: rxVersion, status: rxStatus } = useVersion();
  const rxLabel = rxStatus === "ok" && rxVersion ? `v${rxVersion}` : "…";
  const [scrolled, setScrolled] = React.useState(false);
  const { lang, setLang } = useLang();
  React.useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    window.addEventListener("scroll", onScroll);
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  const NAV_LINKS = [
    { href: "index.html#install",  label: { zh: "安装",     en: "Install" }, priority: "primary" },
    { href: "index.html#agents",   label: { zh: "原理",     en: "How it works" }, priority: "secondary" },
    { href: "index.html#features", label: { zh: "特性",     en: "Features" }, priority: "primary" },
    { href: "index.html#config",   label: { zh: "配置",     en: "Config" }, priority: "secondary" },
    { href: "configuration.html",  label: { zh: "Guide",    en: "Guide" }, priority: "persist" },
    { href: "index.html#roadmap",  label: { zh: "Roadmap",  en: "Roadmap" }, priority: "tertiary" },
    { href: "index.html#faq",      label: { zh: "FAQ",      en: "FAQ" }, priority: "tertiary" },
  ];

  return (
    <nav className="nav" style={scrolled ? { borderBottomColor: "var(--rule-2)" } : {}}>
      <div className="nav-inner">
        <a className="brand" href="index.html">
          <span className="brand-mark"></span>
          <span className="brand-name">
            <b>Reasonix</b><span>DS · {rxLabel}</span>
          </span>
        </a>
        <div className="nav-links" role="navigation">
          {NAV_LINKS.map((l) => (
            <a
              key={t(l.label, "en")}
              href={l.href}
              className={[
                `nav-link-${l.priority}`,
                l.key && active === l.key ? "on" : "",
              ].filter(Boolean).join(" ")}
              style={l.key && active === l.key ? { color: "var(--accent)" } : {}}
            >
              {t(l.label, lang)}
            </a>
          ))}
        </div>
        <div className="nav-cta">
          <div className="lang-switch" role="group" aria-label="Language">
            <button
              type="button"
              className={lang === "en" ? "on" : ""}
              aria-pressed={lang === "en"}
              onClick={() => setLang("en")}
            >EN</button>
            <button
              type="button"
              className={lang === "zh" ? "on" : ""}
              aria-pressed={lang === "zh"}
              onClick={() => setLang("zh")}
            >中文</button>
          </div>
          <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer">
            <Ic.Github size={13}/> GitHub
          </a>
          <a className="btn btn-primary btn-sm" href="download.html">
            {t({ zh: "下载桌面端 →", en: "Download desktop →" }, lang)}
          </a>
        </div>
      </div>
    </nav>
  );
}

window.Nav = Nav;

// Top nav — single bar shared between index and download pages

function Nav({ active }) {
  const [scrolled, setScrolled] = React.useState(false);
  React.useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    window.addEventListener('scroll', onScroll);
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  const NAV_LINKS = [
    { href: 'index.html#install',  label: '安装' },
    { href: 'index.html#agents',   label: '原理' },
    { href: 'index.html#features', label: '特性' },
    { href: 'index.html#config',   label: '配置' },
    { href: 'download.html',       label: '下载', key: 'download' },
    { href: 'index.html#roadmap',  label: 'Roadmap' },
    { href: 'index.html#faq',      label: 'FAQ' },
  ];

  return (
    <nav className="nav" style={scrolled ? { borderBottomColor: 'var(--rule-2)' } : {}}>
      <div className="nav-inner">
        <a className="brand" href="index.html">
          <span className="brand-mark"></span>
          <span className="brand-name">
            <b>Reasonix</b><span>DS · v0.42.0-3</span>
          </span>
        </a>
        <div className="nav-links" role="navigation">
          {NAV_LINKS.map(l => (
            <a
              key={l.label}
              href={l.href}
              className={l.key && active === l.key ? 'on' : ''}
              style={l.key && active === l.key ? {color:'var(--accent)'} : {}}
            >
              {l.label}
            </a>
          ))}
        </div>
        <div className="nav-cta">
          <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer">
            <Ic.Github size={13}/> GitHub
          </a>
          <a className="btn btn-primary btn-sm" href="download.html">
            下载桌面端 →
          </a>
        </div>
      </div>
    </nav>
  );
}

window.Nav = Nav;

// Landing-page entry

function DlPromo() {
  const { lang } = useLang();
  return (
    <div style={{maxWidth:'1280px', margin:'0 auto', padding:'40px 40px 0'}} id="desktop-promo">
      <div className="dl-promo">
        <div>
          <h3 dangerouslySetInnerHTML={{__html: t({
            zh: '或者 — <em>桌面端</em>，开箱即用。',
            en: 'Or — the <em>desktop</em>, no setup required.',
          }, lang)}}/>
          <p>{t({
            zh: '原生 Tauri 客户端 · 自带 Node runtime · 共享 ~/.reasonix 配置。多 tab 会话、实时 cost / cache / token 表盘。',
            en: 'Native Tauri client · bundled Node runtime · shares ~/.reasonix config. Multi-tab sessions, live cost / cache / token meters.',
          }, lang)}</p>
        </div>
        <div className="dl-promo-actions">
          <a className="btn btn-ghost btn-sm" href="download.html">
            {t({ zh: '查看所有平台 →', en: 'All platforms →' }, lang)}
          </a>
          <a className="btn btn-primary btn-sm" href="download.html">
            {t({ zh: '智能镜像下载', en: 'Smart mirror download' }, lang)}
          </a>
        </div>
      </div>
    </div>
  );
}

function App() {
  return (
    <LangProvider>
      <Nav/>
      <Hero/>
      <Install/>
      <DlPromo/>
      <Agents/>
      <Features/>
      <Config/>
      <Community/>
      <Roadmap/>
      <Faq/>
      <Footer/>
    </LangProvider>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App/>);

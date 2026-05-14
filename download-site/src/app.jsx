// Landing-page entry

function DlPromo() {
  return (
    <div style={{maxWidth:'1280px', margin:'0 auto', padding:'40px 40px 0'}} id="desktop-promo">
      <div className="dl-promo">
        <div>
          <h3>或者 — <em>桌面端</em>，开箱即用。</h3>
          <p>
            原生 Tauri 客户端 · 自带 Node runtime · 共享 ~/.reasonix 配置。
            多 tab 会话、实时 cost / cache / token 表盘。
          </p>
        </div>
        <div className="dl-promo-actions">
          <a className="btn btn-ghost btn-sm" href="download.html">
            查看所有平台 →
          </a>
          <a className="btn btn-primary btn-sm" href="download.html">
            智能镜像下载
          </a>
        </div>
      </div>
    </div>
  );
}

function App() {
  return (
    <>
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
    </>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App/>);

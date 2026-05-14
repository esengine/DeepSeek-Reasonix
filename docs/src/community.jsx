// Community — live GitHub stats + contributors from the API

function useGithubStats() {
  const [stats, setStats] = React.useState({ stars: null, forks: null, openIssues: null });
  const [contributors, setContributors] = React.useState([]);
  React.useEffect(() => {
    let cancelled = false;
    fetch("https://api.github.com/repos/esengine/DeepSeek-Reasonix")
      .then(r => r.ok ? r.json() : null)
      .then(j => {
        if (cancelled || !j) return;
        setStats({
          stars: j.stargazers_count ?? null,
          forks: j.forks_count ?? null,
          openIssues: j.open_issues_count ?? null,
        });
      })
      .catch(() => {});
    fetch("https://api.github.com/repos/esengine/DeepSeek-Reasonix/contributors?per_page=24")
      .then(r => r.ok ? r.json() : null)
      .then(j => {
        if (cancelled || !Array.isArray(j)) return;
        setContributors(j);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, []);
  return { stats, contributors };
}

function Community() {
  const { stats, contributors } = useGithubStats();
  const { lang } = useLang();
  return (
    <section className="section" id="community">
      <SecHead
        num="06"
        label="Community"
        title={t({
          zh: '由 <em>社区</em>驱动，完全开源。',
          en: '<em>Community-driven</em>, fully open source.',
        }, lang)}
        sub={t({
          zh: 'MIT 协议、公开 Roadmap、公开 Discussions。欢迎一起把推理型 Agent 的工程基线推得更高。',
          en: 'MIT-licensed, public roadmap, open Discussions. Help push the engineering baseline for reasoning-type agents higher.',
        }, lang)}
      />

      <div className="community-grid">
        <div className="star-card">
          <div style={{display:"flex",alignItems:"center",gap:10,color:"var(--cream-mute)",fontFamily:"var(--mono)",fontSize:11,letterSpacing:"0.08em",textTransform:"uppercase"}}>
            <Ic.Star size={12}/> Github Stats · live
          </div>
          <div className="star-row">
            <b>{stats.stars != null ? stats.stars.toLocaleString() : "—"}</b>
            <span className="delta">stars</span>
            <span style={{marginLeft:"auto", fontFamily:"var(--mono)", fontSize:12, color:"var(--cream-mute)"}}>
              {stats.forks != null && stats.openIssues != null
                ? `· forks ${stats.forks} · open issues ${stats.openIssues}`
                : "· loading…"}
            </span>
          </div>
          <div style={{display:"flex", gap:10, marginTop:18, flexWrap:"wrap"}}>
            <a className="btn btn-primary btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer">
              <Ic.Star size={13}/> Star on GitHub
            </a>
            <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix" target="_blank" rel="noreferrer">
              <Ic.Github size={13}/> {t({ zh: '阅读源码', en: 'Read source' }, lang)}
            </a>
            <a className="btn btn-ghost btn-sm" href="https://github.com/esengine/DeepSeek-Reasonix/discussions" target="_blank" rel="noreferrer">
              {t({ zh: '加入讨论', en: 'Join discussions' }, lang)}
            </a>
          </div>
        </div>

        <div className="contrib-grid">
          <h3>{t({ zh: '开源贡献者', en: 'Open-source contributors' }, lang)}</h3>
          <div className="sub">core · plugin · docs · translation</div>
          <div className="contrib-wall">
            {contributors.slice(0, 24).map((c) => (
              <a
                key={c.login}
                href={c.html_url}
                target="_blank"
                rel="noreferrer"
                className="contrib-avatar"
                title={c.login}
                style={{
                  backgroundImage: c.avatar_url ? `url(${c.avatar_url})` : undefined,
                  backgroundSize: "cover",
                  color: "transparent",
                }}
              >{c.login.slice(0, 2).toUpperCase()}</a>
            ))}
            {contributors.length === 0 && (
              <div className="contrib-avatar" style={{background:"transparent", border:"1px solid var(--rule-2)", color:"var(--cream-mute)", fontSize:11}}>loading…</div>
            )}
            {contributors.length > 0 && (
              <a
                href="https://github.com/esengine/DeepSeek-Reasonix/graphs/contributors"
                target="_blank"
                rel="noreferrer"
                className="contrib-avatar"
                style={{background:"transparent", border:"1px solid var(--rule-2)", color:"var(--cream-dim)"}}
              >+more</a>
            )}
          </div>
          <p style={{color:"var(--cream-mute)", fontSize:12.5, marginTop:18, marginBottom:0, lineHeight:1.6}}>
            {t({
              zh: <>想成为下一个？阅读 <a href="https://github.com/esengine/DeepSeek-Reasonix/blob/main/CONTRIBUTING.md" target="_blank" rel="noreferrer" style={{color:"var(--accent)", textDecoration:"none"}}>CONTRIBUTING.md</a> 并领取一个 <span className="kbd">good first issue</span>。</>,
              en: <>Want to be next? Read <a href="https://github.com/esengine/DeepSeek-Reasonix/blob/main/CONTRIBUTING.md" target="_blank" rel="noreferrer" style={{color:"var(--accent)", textDecoration:"none"}}>CONTRIBUTING.md</a> and pick up a <span className="kbd">good first issue</span>.</>,
            }, lang)}
          </p>
        </div>
      </div>
    </section>
  );
}

window.Community = Community;

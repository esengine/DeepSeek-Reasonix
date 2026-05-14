// FAQ — based on actual README "what Reasonix deliberately doesn't do" section

const FAQS = [
  {
    q: '为什么只支持 DeepSeek？能不能换 Claude / GPT？',
    a: '这不是限制，是设计。DeepSeek 的 prefix-cache 从 prompt 第 0 字节开始指纹化，Reasonix 的循环是围绕这个不变量构建的 —— 长会话能保持 ~94% 缓存命中。挂到 Anthropic 兼容端点能拿到便宜 token，但 cache_control 标记会失效；通用 backend (Aider / Cline / Continue) 的压缩模式则会破坏字节稳定性。Coupling to one backend is the feature。',
  },
  {
    q: '需要付费吗？',
    a: 'Reasonix 本身 MIT 开源，完全免费。但需要付费的 DeepSeek API Key。参考定价：V4-Flash $0.07/Mtok 未命中、$0.014/Mtok 命中，长会话下成本通常只到通用工具的 1/3。',
  },
  {
    q: '需要 IDE 插件吗？',
    a: '不会做。Reasonix 是 terminal-first。diff 留给 git diff，文件树留给 ls。桌面端是配套的可视化伴侣，不是 Cursor 替代品。',
  },
  {
    q: '能在内网 / 私有部署的 DeepSeek 上跑吗？',
    a: '可以。从 0.30 起接受非标准 key 前缀的自托管 DeepSeek 端点。把 baseUrl 改成你的内部地址即可，循环、缓存策略、工具协议都不变。',
  },
  {
    q: 'CLI 和桌面端是什么关系？',
    a: '完全同一份循环 / 协议 / ~/.reasonix 配置。桌面端 (Tauri) 自带 Node runtime，无需独立 npm install；多 tab 会话、右侧栏列出当前会话读过和改过的文件，底部显示 cost / cache / token 实时表盘。',
  },
  {
    q: '怎么开发自己的 Skill？',
    a: '没有远程注册表，直接写文件。在 TUI 内 /skill new my-skill 生成项目级模板，--global 写到 ~/.reasonix/skills 跨项目复用。Skill 是带 frontmatter (description, runAs, allowed-tools) 的 Markdown，runAs: subagent 会在隔离子循环里运行。',
  },
  {
    q: '工具调用是否安全？',
    a: '所有原生工具 (read_file / write_file / edit_file / run_command 等) 都沙箱化到启动目录，--dir 显式指定。SEARCH/REPLACE 编辑默认进 pending 队列，/apply 才落盘。/plan 进入只读审计门，未批准计划前不允许写入。',
  },
  {
    q: '能切换工作目录吗？',
    a: '不能在 session 中途切。memory 路径会与陈旧的根目录纠缠。退出后 reasonix code --dir <path> 重新启动即可。',
  },
];

function Faq() {
  const [open, setOpen] = React.useState(0);
  return (
    <section className="section" id="faq">
      <SecHead
        num="08"
        label="FAQ"
        title="高频<em>问题</em>。"
        sub="仍有疑问？欢迎到 GitHub Discussions 提问。"
      />

      <div className="faq-list">
        {FAQS.map((f, i) => (
          <div key={i} className={'faq-item ' + (open === i ? 'open' : '')}>
            <div className="faq-q" onClick={() => setOpen(open === i ? -1 : i)}>
              <span className="idx">{String(i + 1).padStart(2, '0')}</span>
              <span style={{flex:1}}>{f.q}</span>
              <Ic.Chev className="chev"/>
            </div>
            <div className="faq-a">{f.a}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

window.Faq = Faq;

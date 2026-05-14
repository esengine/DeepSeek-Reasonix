// FAQ — based on actual README "what Reasonix deliberately doesn't do" section

const FAQS = [
  {
    q: { zh: '为什么只支持 DeepSeek？能不能换 Claude / GPT？', en: 'Why DeepSeek only? Can I swap to Claude / GPT?' },
    a: {
      zh: '这不是限制，是设计。DeepSeek 的 prefix-cache 从 prompt 第 0 字节开始指纹化，Reasonix 的循环是围绕这个不变量构建的 —— 长会话能保持 ~94% 缓存命中。挂到 Anthropic 兼容端点能拿到便宜 token，但 cache_control 标记会失效；通用 backend (Aider / Cline / Continue) 的压缩模式则会破坏字节稳定性。Coupling to one backend is the feature。',
      en: 'It\'s a design choice, not a limitation. DeepSeek\'s prefix cache fingerprints prompts from byte 0; the Reasonix loop is engineered around that invariant — long sessions hold ~94% cache hit. Pointing at an Anthropic-compatible endpoint gets you cheap tokens but breaks the cache_control markers. Generic backends (Aider / Cline / Continue) compress history, which destroys byte stability. Coupling to one backend is the feature.',
    },
  },
  {
    q: { zh: '需要付费吗？', en: 'Is it free?' },
    a: {
      zh: 'Reasonix 本身 MIT 开源，完全免费。但需要付费的 DeepSeek API Key。参考定价：V4-Flash $0.07/Mtok 未命中、$0.014/Mtok 命中，长会话下成本通常只到通用工具的 1/3。',
      en: 'Reasonix itself is MIT-licensed and free. The DeepSeek API key is paid: V4-Flash is $0.07/Mtok uncached and $0.014/Mtok cached. In long sessions the bill typically lands at ~1/3 of comparable generic tooling.',
    },
  },
  {
    q: { zh: '需要 IDE 插件吗？', en: 'Will there be an IDE plugin?' },
    a: {
      zh: '不会做。Reasonix 是 terminal-first。diff 留给 git diff，文件树留给 ls。桌面端是配套的可视化伴侣，不是 Cursor 替代品。',
      en: 'No. Reasonix is terminal-first. `git diff` does diffs; `ls` does file trees. The desktop is a visual companion to the CLI, not a Cursor replacement.',
    },
  },
  {
    q: { zh: '能在内网 / 私有部署的 DeepSeek 上跑吗？', en: 'Can I point it at a self-hosted / private DeepSeek endpoint?' },
    a: {
      zh: '可以。从 0.30 起接受非标准 key 前缀的自托管 DeepSeek 端点。把 baseUrl 改成你的内部地址即可，循环、缓存策略、工具协议都不变。',
      en: 'Yes. Since 0.30 we accept non-standard key prefixes for self-hosted DeepSeek endpoints. Just point `baseUrl` at your internal address — the loop, cache strategy, and tool protocol are unchanged.',
    },
  },
  {
    q: { zh: 'CLI 和桌面端是什么关系？', en: 'How does the CLI relate to the desktop?' },
    a: {
      zh: '完全同一份循环 / 协议 / ~/.reasonix 配置。桌面端 (Tauri) 自带 Node runtime，无需独立 npm install；多 tab 会话、右侧栏列出当前会话读过和改过的文件，底部显示 cost / cache / token 实时表盘。',
      en: 'Same loop, same protocol, same `~/.reasonix` config. The desktop (Tauri) bundles its own Node runtime — no separate npm install. Multi-tab sessions, side panel listing files this session read or wrote, live cost / cache / token meters along the bottom.',
    },
  },
  {
    q: { zh: '怎么开发自己的 Skill？', en: 'How do I write my own skill?' },
    a: {
      zh: '没有远程注册表，直接写文件。在 TUI 内 /skill new my-skill 生成项目级模板，--global 写到 ~/.reasonix/skills 跨项目复用。Skill 是带 frontmatter (description, runAs, allowed-tools) 的 Markdown，runAs: subagent 会在隔离子循环里运行。',
      en: 'No remote registry — just write a file. Inside the TUI run `/skill new my-skill` to scaffold a project-local skill; add `--global` to put it under `~/.reasonix/skills` for reuse across projects. Skills are Markdown with frontmatter (description, runAs, allowed-tools); `runAs: subagent` runs the body in an isolated sub-loop.',
    },
  },
  {
    q: { zh: '工具调用是否安全？', en: 'Are tool calls safe?' },
    a: {
      zh: '所有原生工具 (read_file / write_file / edit_file / run_command 等) 都沙箱化到启动目录，--dir 显式指定。SEARCH/REPLACE 编辑默认进 pending 队列，/apply 才落盘。/plan 进入只读审计门，未批准计划前不允许写入。',
      en: 'Every built-in tool (`read_file` / `write_file` / `edit_file` / `run_command` …) is sandboxed to the launch directory or whatever you set via `--dir`. SEARCH/REPLACE edits queue as pending; nothing hits disk until you `/apply`. `/plan` mode is a read-only audit gate — no writes allowed until the plan is approved.',
    },
  },
  {
    q: { zh: '能切换工作目录吗？', en: 'Can I switch working directories mid-session?' },
    a: {
      zh: '不能在 session 中途切。memory 路径会与陈旧的根目录纠缠。退出后 reasonix code --dir <path> 重新启动即可。',
      en: 'No — memory paths would tangle with the stale root. Exit and relaunch with `reasonix code --dir <path>`.',
    },
  },
];

function Faq() {
  const [open, setOpen] = React.useState(0);
  const { lang } = useLang();
  return (
    <section className="section" id="faq">
      <SecHead
        num="08"
        label="FAQ"
        title={t({ zh: '高频<em>问题</em>。', en: 'Frequently <em>asked</em>.' }, lang)}
        sub={t({
          zh: '仍有疑问？欢迎到 GitHub Discussions 提问。',
          en: 'Still stuck? Open a thread in GitHub Discussions.',
        }, lang)}
      />

      <div className="faq-list">
        {FAQS.map((f, i) => (
          <div key={i} className={'faq-item ' + (open === i ? 'open' : '')}>
            <div className="faq-q" onClick={() => setOpen(open === i ? -1 : i)}>
              <span className="idx">{String(i + 1).padStart(2, '0')}</span>
              <span style={{flex:1}}>{t(f.q, lang)}</span>
              <Ic.Chev className="chev"/>
            </div>
            <div className="faq-a">{t(f.a, lang)}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

window.Faq = Faq;

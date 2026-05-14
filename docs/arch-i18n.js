/* Architecture page translations + scrollspy. Layered on top of i18n.js. */

(function () {
  "use strict";

  var R = window.Reasonix;
  if (!R) return;

  var en = {
    "arch.badge": "Cache · Repair · Cost · Modules",
    "arch.title.line1": "Reasonix Architecture",
    "arch.title.line2": "how it works under the hood",
    "arch.sub":
      "Reasonix is <strong>opinionated, not general</strong>. Every abstraction is justified by a DeepSeek-specific behavior or economic property. The product north star: a coding agent that stays cheap enough to leave on.",

    "arch.toc.title": "On this page",
    "arch.toc.philosophy": "Design philosophy",
    "arch.toc.pillar1": "Pillar 1 — Cache",
    "arch.toc.pillar2": "Pillar 2 — Repair",
    "arch.toc.pillar3": "Pillar 3 — Cost",
    "arch.toc.modules": "Module layout",
    "arch.toc.evolution": "Design evolution",
    "arch.toc.nongoals": "Non-goals",

    "th.var": "Env var",
    "th.default": "Default",
    "th.effect": "Effect",
    "th.preset": "Preset",
    "th.model": "Model",
    "th.effort": "Effort",
    "th.cost": "Cost",

    "ph.title": "Design philosophy",
    "ph.body1":
      "Reasonix is <strong>opinionated, not general</strong>. Every abstraction is justified by a DeepSeek-specific behavior or economic property. If it's generic, we don't ship it.",
    "ph.body2":
      "The product north star: <strong>coding agent that stays cheap enough to leave on</strong>. A tool that quietly burns $200/month on a background project is one nobody uses. Every subsystem below is answerable to that goal.",

    "p1.title": "Pillar 1 — Cache-First Loop",
    "p1.problem":
      "<strong>Problem.</strong> DeepSeek bills cached input at ~10% of the miss rate. Automatic prefix caching activates only when the <em>exact</em> byte prefix of the previous request matches. Most agent loops reorder, rewrite, or inject fresh timestamps each turn — cache hit rate in practice: &lt;20%.",
    "p1.solution":
      "<strong>Solution.</strong> Partition the context into three regions:",
    "p1.inv.title": "Invariants:",
    "p1.inv1": "Prefix is computed once per session, hashed, and pinned.",
    "p1.inv2": "Log entries are serialized in append order; no rewrites.",
    "p1.inv3":
      "Scratch is distilled via Pillar 2 before any information from it is folded into the log.",
    "p1.metric":
      "<strong>Metric.</strong> <code>prompt_cache_hit_tokens / (hit + miss)</code> exposed per-turn and aggregated per-session. Visible in the TUI's top-bar cache cell.",
    "p1.h.parallel": "Parallel tool dispatch",
    "p1.parallel.body":
      "Each tool declares <code>parallelSafe?: boolean</code> (default <code>false</code>). The loop dispatcher groups consecutive parallel-safe calls into chunks and races them via <code>Promise.allSettled</code>; the first non-parallel-safe call ends the chunk and runs alone (serial barrier — read-after-write order preserved). Tool-result yields and history append still land in declared order regardless of which call settles first, so the model sees the same shape it would under a fully serial dispatch.",
    "p1.parallel.optins":
      "Built-in opt-ins: read-only filesystem (<code>read_file</code>, <code>list_directory</code>, <code>directory_tree</code>, <code>search_files</code>, <code>search_content</code>, <code>get_file_info</code>), web (<code>web_search</code>, <code>web_fetch</code>), <code>recall_memory</code>, <code>semantic_search</code>, isolated child loops (<code>run_skill</code>, <code>spawn_subagent</code>), in-memory job queries (<code>job_output</code>, <code>list_jobs</code>). Mutating / side-effecting tools stay default. MCP-bridged tools default <code>false</code> — third-party tools opt in only when the server explicitly declares parallel safety.",

    "p2.title": "Pillar 2 — Tool-Call Repair",
    "p2.problem": "<strong>Problem.</strong> Empirical DeepSeek failure modes:",
    "p2.fm1":
      "Tool-call JSON emitted inside <code>&lt;think&gt;</code>, missing from the final message.",
    "p2.fm2":
      "Arguments dropped when schema has &gt;10 params or deeply nested objects.",
    "p2.fm3": "Same tool called repeatedly with identical args (call-storm).",
    "p2.fm4":
      "Truncated JSON due to <code>max_tokens</code> hit mid-structure.",
    "p2.solution": "<strong>Solution.</strong> Four passes:",
    "p2.pass1":
      "<strong><code>flatten</code></strong> — schemas with &gt;10 leaf params or depth &gt;2 are auto-detected on <code>ToolRegistry.register()</code> and presented to the model in dot-notation form. <code>dispatch()</code> re-nests the args before calling the user's <code>fn</code>.",
    "p2.pass2":
      "<strong><code>scavenge</code></strong> — regex + JSON parser sweeps <code>reasoning_content</code> for any tool call the model forgot to emit in <code>tool_calls</code>.",
    "p2.pass3":
      "<strong><code>truncation</code></strong> — detect unbalanced JSON and repair by closing braces or requesting a continuation completion.",
    "p2.pass4":
      "<strong><code>storm</code></strong> — identical <code>(tool, args)</code> tuple within a sliding window → suppress the call, inject a reflection turn.",

    "p3.title": "Pillar 3 — Cost Control (v0.6)",
    "p3.problem":
      "<strong>Problem.</strong> Coding agents that default to the frontier model (v4-pro, ~12× flash cost) and accumulate full tool results in context are $150–$250/month for active users. Most turns don't need frontier reasoning; most sessions re-pay for tool results that were only useful once.",
    "p3.solution":
      "<strong>Solution.</strong> Four complementary mechanisms, none of which require manual tuning in the common case:",
    "p3.h.tiers": "4.1 Tiered defaults (flash-first)",
    "p3.tiers.body":
      "The three presets trade <strong>model tier</strong> and <strong>reasoning effort</strong>:",
    "p3.tiers.aux":
      "All auxiliary calls — <code>forceSummaryAfterIterLimit</code>, subagent spawns, truncation repair retries — hard-code <code>v4-flash + effort=high</code> regardless of the user's preset. There's no reason to pay pro rates for paraphrasing tool results or for an <code>explore</code> subagent's grep chain.",
    "p3.h.compact": "4.2 Turn-end auto-compaction",
    "p3.compact.body":
      "Every tool result in the log exceeding <code>TURN_END_RESULT_CAP_TOKENS</code> (3000) is shrunk to that cap when a turn ends. The model had the full text for the turn that read it; subsequent turns see a compact summary and can re-read if needed. One extra <code>read_file</code> call is vastly cheaper than dragging 12 KB through every future prompt.",
    "p3.compact.proactive":
      "A proactive 40% context-ratio threshold runs the same shrink pre-emptively inside long multi-iter turns before the 80% emergency threshold fires.",
    "p3.h.pro": "4.3 /pro single-turn arming",
    "p3.pro.body":
      "Users who predict a hard task type <code>/pro</code>; the <strong>next</strong> turn runs on <code>v4-pro</code>, then auto-disarms. No preset churn, no forgotten revert. Armed state is visible as a yellow <code>⇧ pro armed</code> pill in the header.",
    "p3.h.escalation": "4.4 Failure-signal auto-escalation",
    "p3.esc.body":
      "The loop counts visible "flash is struggling" events per turn:",
    "p3.esc.e1":
      "<code>edit_file</code> / <code>write_file</code> SEARCH-not-found errors",
    "p3.esc.e2":
      "ToolCallRepair fires (scavenge / truncation-fix / storm-break)",
    "p3.esc.threshold":
      "Once the count hits <code>FAILURE_ESCALATION_THRESHOLD</code> (3), the <strong>remainder of the current turn</strong> runs on <code>v4-pro</code>. Announced via a yellow warning row — no silent cost surprises. Counter + escalation flag reset at every turn start.",
    "p3.esc.pill":
      "Header shows a red <code>⇧ pro escalated</code> pill while the turn is on pro.",
    "p3.h.transparency": "Cost transparency",
    "p3.trans.body":
      "Per-turn and session cost are colored in the StatsPanel:",
    "p3.trans.turn":
      "<code>turn $0.003</code> — green &lt;$0.05, yellow $0.05–0.20, red ≥$0.20",
    "p3.trans.session": "<code>session $0.12</code> — same scale ×10",

    "mod.title": "Module layout",
    "mod.note":
      "Files kept small by design: the largest module under <code>cli/ui/</code> is 2K lines (App.tsx), every handler under <code>slash/handlers/</code> is ≤200 lines, every hook under <code>cli/ui/</code> is ≤310 lines. Adding a new slash command means editing one handler file and one registry line.",

    "evo.title": "Design evolution",

    "ng.title": "Explicit non-goals",
    "ng.item1":
      "Multi-agent orchestration as a first-class concept (subagents are a cost-reduction mechanism, not a coordination primitive).",
    "ng.item2": "RAG / vector retrieval.",
    "ng.item3":
      "Support for non-DeepSeek backends (an OpenAI-compatible shim would work today via <code>--model</code> override, but is not tested).",
    "ng.item4": "Web UI / SaaS.",
    "ng.item5":
      "Automatic cost escalation without user-visible announcement. Every pro-tier model call is surfaced; silent escalation was considered and rejected.",
  };

  var zh = {
    "arch.badge": "缓存 · 修复 · 成本 · 模块",
    "arch.title.line1": "Reasonix 架构",
    "arch.title.line2": "底层原理详解",
    "arch.sub":
      "Reasonix <strong>有明确取舍，不追求通用</strong>。每一个抽象都有对应的 DeepSeek 特有行为或经济特性作为依据。产品北极星：一个便宜到可以一直挂着跑的编程 Agent。",

    "arch.toc.title": "本页目录",
    "arch.toc.philosophy": "设计哲学",
    "arch.toc.pillar1": "支柱一 — 缓存优先循环",
    "arch.toc.pillar2": "支柱二 — 工具调用修复",
    "arch.toc.pillar3": "支柱三 — 成本控制",
    "arch.toc.modules": "模块布局",
    "arch.toc.evolution": "设计演进",
    "arch.toc.nongoals": "明确不做的事",

    "th.var": "环境变量",
    "th.default": "默认值",
    "th.effect": "效果",
    "th.preset": "预设",
    "th.model": "模型",
    "th.effort": "推理力度",
    "th.cost": "费用",

    "ph.title": "设计哲学",
    "ph.body1":
      "Reasonix <strong>有明确取舍，不追求通用</strong>。每一个抽象都有对应的 DeepSeek 特有行为或经济特性作为依据。不通用的，就不加进来。",
    "ph.body2":
      "产品北极星：<strong>便宜到可以一直挂着跑的编程 Agent</strong>。一个在后台项目上悄悄烧掉 $200/月的工具没人会用。以下每一个子系统都要对这个目标负责。",

    "p1.title": "支柱一 — 缓存优先循环",
    "p1.problem":
      "<strong>问题。</strong>DeepSeek 对命中缓存的输入 token 只收约 10% 的费用。自动前缀缓存只在<em>字节级完全匹配</em>上一次请求的前缀时才激活。大多数 agent 循环每轮都会重排、改写或注入新时间戳——实际缓存命中率：&lt;20%。",
    "p1.solution":
      "<strong>解决方案。</strong>把上下文分成三个区域：",
    "p1.inv.title": "不变量：",
    "p1.inv1": "前缀在每个 session 开始时计算一次，哈希后钉住。",
    "p1.inv2": "日志条目按追加顺序序列化，不允许改写。",
    "p1.inv3":
      "Scratch 中的信息先经过支柱二蒸馏，再折叠进日志。",
    "p1.metric":
      "<strong>指标。</strong><code>prompt_cache_hit_tokens / (hit + miss)</code> 每轮暴露并按 session 聚合，显示在 TUI 顶栏的缓存格。",
    "p1.h.parallel": "并行工具调度",
    "p1.parallel.body":
      "每个工具声明 <code>parallelSafe?: boolean</code>（默认 <code>false</code>）。循环调度器把连续的可并行调用打成一批，通过 <code>Promise.allSettled</code> 并发执行；第一个不可并行的调用截断当前批次并单独串行执行（串行屏障——保证读后写顺序）。工具结果 yield 和历史追加仍按声明顺序落入，模型看到的形状与完全串行一致。",
    "p1.parallel.optins":
      "内置并行安全工具：只读文件系统（<code>read_file</code>、<code>list_directory</code>、<code>directory_tree</code>、<code>search_files</code>、<code>search_content</code>、<code>get_file_info</code>）、Web（<code>web_search</code>、<code>web_fetch</code>）、<code>recall_memory</code>、<code>semantic_search</code>、隔离子循环（<code>run_skill</code>、<code>spawn_subagent</code>）、内存中任务查询（<code>job_output</code>、<code>list_jobs</code>）。有副作用的工具保持默认 false。MCP 桥接工具默认 false——第三方工具只有服务端明确声明并行安全时才可选入。",

    "p2.title": "支柱二 — 工具调用修复",
    "p2.problem": "<strong>问题。</strong>DeepSeek 实测失败模式：",
    "p2.fm1":
      "工具调用 JSON 被包在 <code>&lt;think&gt;</code> 里，没有出现在 final message。",
    "p2.fm2":
      "schema 有 &gt;10 个参数或深层嵌套时参数被丢弃。",
    "p2.fm3": "用相同参数重复调用同一工具（调用风暴）。",
    "p2.fm4":
      "因 <code>max_tokens</code> 在 JSON 结构中间截断导致 JSON 不完整。",
    "p2.solution": "<strong>解决方案。</strong>四个修复 pass：",
    "p2.pass1":
      "<strong><code>flatten</code></strong> — 超过 10 个叶节点参数或嵌套深度 &gt;2 的 schema 在 <code>ToolRegistry.register()</code> 时自动检测，以点记法平铺展示给模型。<code>dispatch()</code> 在调用用户 <code>fn</code> 前重新嵌套参数。",
    "p2.pass2":
      "<strong><code>scavenge</code></strong> — 用正则 + JSON 解析器扫描 <code>reasoning_content</code>，捞回模型忘记放进 <code>tool_calls</code> 的工具调用。",
    "p2.pass3":
      "<strong><code>truncation</code></strong> — 检测不平衡 JSON，通过补全括号或请求续写来修复。",
    "p2.pass4":
      "<strong><code>storm</code></strong> — 滑动窗口内出现相同 <code>(tool, args)</code> 元组时压制该调用并注入反思轮。",

    "p3.title": "支柱三 — 成本控制（v0.6）",
    "p3.problem":
      "<strong>问题。</strong>默认使用旗舰模型（v4-pro，约 12× flash 成本）并在上下文中积累完整工具结果的编程 agent，活跃用户每月 $150–$250。大多数轮次不需要旗舰推理；大多数 session 都在重复支付只用了一次的工具结果。",
    "p3.solution":
      "<strong>解决方案。</strong>四个互补机制，常规场景下无需手动调整：",
    "p3.h.tiers": "4.1 分层默认（flash 优先）",
    "p3.tiers.body":
      "三个预设在<strong>模型层级</strong>和<strong>推理力度</strong>之间做权衡：",
    "p3.tiers.aux":
      "所有辅助调用——<code>forceSummaryAfterIterLimit</code>、subagent 派生、截断修复重试——无论用户预设如何，均硬编码为 <code>v4-flash + effort=high</code>。没有理由以 pro 价格来"把工具结果改写成散文"或跑 <code>explore</code> subagent 的 grep 链。",
    "p3.h.compact": "4.2 轮末自动压缩",
    "p3.compact.body":
      "每轮结束时，日志中超过 <code>TURN_END_RESULT_CAP_TOKENS</code>（3000）的工具结果会被压缩到该上限。读它的那一轮模型拿到了完整文本；后续轮次看到的是紧凑摘要，需要时可以重新读取。多一次 <code>read_file</code> 调用远比每次 prompt 都拖 12 KB 便宜。",
    "p3.compact.proactive":
      "当上下文占比达到 40% 时会主动提前触发相同压缩，而不是等到 80% 的紧急阈值才处理。",
    "p3.h.pro": "4.3 /pro 单轮唤醒",
    "p3.pro.body":
      "预判当前任务较难时输入 <code>/pro</code>；<strong>下一轮</strong>在 <code>v4-pro</code> 上运行，随后自动解除。无需反复切预设，不会忘记还原。标头会显示黄色 <code>⇧ pro armed</code> 标签。",
    "p3.h.escalation": "4.4 失败信号自动升级",
    "p3.esc.body":
      "循环统计每轮的"flash 在挣扎"事件：",
    "p3.esc.e1":
      "<code>edit_file</code> / <code>write_file</code> 的 SEARCH-not-found 错误",
    "p3.esc.e2":
      "ToolCallRepair 触发（scavenge / truncation-fix / storm-break）",
    "p3.esc.threshold":
      "计数达到 <code>FAILURE_ESCALATION_THRESHOLD</code>（3）后，<strong>当前轮的剩余部分</strong>切换到 <code>v4-pro</code>，并通过黄色警告行通知用户——不会有静默涨费。计数器和升级标志在每轮开始时重置。",
    "p3.esc.pill":
      "该轮在 pro 上运行时，标头显示红色 <code>⇧ pro escalated</code> 标签。",
    "p3.h.transparency": "成本透明度",
    "p3.trans.body":
      "StatsPanel 中按颜色显示每轮和 session 成本：",
    "p3.trans.turn":
      "<code>turn $0.003</code> — 绿色 &lt;$0.05，黄色 $0.05–0.20，红色 ≥$0.20",
    "p3.trans.session": "<code>session $0.12</code> — 同比例 ×10",

    "mod.title": "模块布局",
    "mod.note":
      "文件故意保持精简：<code>cli/ui/</code> 下最大的模块是 2K 行（App.tsx），<code>slash/handlers/</code> 下每个 handler ≤200 行，<code>cli/ui/</code> 下每个 hook ≤310 行。新增斜杠命令只需改一个 handler 文件和一行注册。",

    "evo.title": "设计演进",

    "ng.title": "明确不做的事",
    "ng.item1":
      "把多 agent 编排作为一等概念（subagent 是成本削减机制，不是协调原语）。",
    "ng.item2": "RAG / 向量检索。",
    "ng.item3":
      "支持非 DeepSeek 后端（通过 <code>--model</code> 覆盖使用 OpenAI 兼容 shim 今天能跑，但不做测试保障）。",
    "ng.item4": "Web UI / SaaS。",
    "ng.item5":
      "不经用户可见通知的自动成本升级。每一次 pro 级模型调用都会明示；静默升级方案讨论过，被否决。",
  };

  var DICT = { en: en, zh: zh };

  function applyArch(lang) {
    var dict = DICT[lang] || DICT.en;
    document.querySelectorAll("[data-i18n]").forEach(function (el) {
      var key = el.getAttribute("data-i18n");
      if (dict[key] !== undefined) el.innerHTML = dict[key];
    });
  }

  applyArch(R.lang());
  R.onLangChange(applyArch);

  var sections = Array.prototype.slice.call(
    document.querySelectorAll(".guide-body section[id]"),
  );
  var tocLinks = Array.prototype.slice.call(
    document.querySelectorAll(".guide-toc a"),
  );
  if (sections.length && tocLinks.length && "IntersectionObserver" in window) {
    var byId = {};
    tocLinks.forEach(function (a) {
      var href = a.getAttribute("href") || "";
      var id = href.replace(/^#/, "");
      if (id) byId[id] = a;
    });
    var io = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (e) {
          var link = byId[e.target.id];
          if (!link) return;
          if (e.isIntersecting) {
            tocLinks.forEach(function (l) {
              l.classList.remove("is-active");
            });
            link.classList.add("is-active");
          }
        });
      },
      { rootMargin: "-30% 0px -60% 0px", threshold: 0 },
    );
    sections.forEach(function (s) {
      io.observe(s);
    });
  }
})();

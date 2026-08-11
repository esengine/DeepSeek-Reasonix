# Bug 回归防范体系（REGRESSION GUARD）

> 把"修好的 bug"变成"再也修不坏的约束"。
> 本文档按 Reasonix 自身工程理念组织（REASONIX.md 六条），
> 是 Reasonix 及旗下工具仓库（unified-rx-mcp 等）的防回归总纲。
> 2026-08-11 初稿，案例全部来自 unified-rx-mcp 真实修复。

---

## 0. 为什么 bug 会回来

修一个 bug 只改代码，bug 就会回来——因为**回来的不是代码，是产生 bug 的条件**。
四个真实案例（unified-rx-mcp，2026-08-11 修复）：

| # | 已修 bug | 修复提交 | 为什么可能复发 |
|---|---|---|---|
| A | MCP 启动崩溃：async handler 内 `asyncio.run()`（事件循环内调用必炸） | `2952aa5` | pytest 直接调函数测不到协议层，真实 MCP 启动才炸 |
| B | 测试污染生产 state：LSE 测试写入 `~/.unified-rx/lse-state.json` | `89e8180` | 隔离靠一个 fixture，删掉或改路径就失效，无效果断言 |
| C | 工具数声明漂移：README/plugin 手写 61→64→48，与实际不符 | 多提交 | 数字人肉维护，任何人改 `_TOOLS` 都会再次漂移 |
| D | workflow 配置错误：CodeQL 矩阵含仓库没有的语言、zizmor 缺权限、REUSE 缺 SPDX 头 | `ee58b31`/`5198692` | 本地无预检，全靠 CI 跑完才发现；actionlint 还是软门禁 |

**共同规律**：每个 bug 都缺一个"能在它发生前抓住它的检查"。

---

## 1. 理念映射：RX 六条理念 → 防回归机制

Reasonix 的工程理念本身就是答案——把约束变成**可执行的检查**，不靠人记住。

| RX 理念（REASONIX.md） | 防回归机制 | 落地载体 |
|---|---|---|
| **代码即真理**（注释只写 why） | 修 bug 必须同步提交"能抓住它的检查"，无检查的修复不叫修复 | PR 门禁：`Regression-guard: <检查名>` 字段 |
| **约束进工具**（repolint 棘轮基线：旧债容忍、新问题失败 CI、永不扩基线） | `tool_ratchet`：工具清单基线文件，改 `_TOOLS` 必须同步更新基线，否则 CI 红 | `scripts/tool_ratchet.py` + `tools.json` |
| **效果测试**（组件正确 ≠ 系统有效；`internal/boot/effect_test.go` 模式） | 协议层冒烟（`mcp_smoke.py`）测"真实启动 + tools/list"，不是测函数 | `scripts/mcp_smoke.py` 接入 CI |
| **缓存前缀 byte-stable**（禁止中途变异） | `_DEFS_CACHE` 只读契约测试：同进程内两次 `_definitions()` 返回逐字节一致 | pytest 契约测试 |
| **Pre-push CI 模拟**（本地先跑 CI 最快失败项） | 本地四连：pytest → cargo test → mcp_smoke → vuln-scan，提交前必跑 | `scripts/pre-push.sh` |
| **PR 元数据门禁**（脚本是 source of truth） | actionlint/zizmor 去 `continue-on-error` 变硬门禁；workflow 改动本地预检 | `scripts/wf_check.sh` |

---

## 2. 三层防回归架构

```
┌─ 门禁层（提交/CI 拒绝）─────────────────────────────┐
│  CI 四连：pytest + cargo test + mcp_smoke + vuln-scan │
│  actionlint/zizmor 硬门禁（无 continue-on-error）     │
│  tool_ratchet 基线校验（工具数/清单漂移即红）          │
├─ 检查层（本地静态，秒级）───────────────────────────┤
│  async_guard：扫描同步路径禁用 asyncio.run 的调用链   │
│  wf_check：workflow 语言矩阵/权限/REUSE 头预检        │
│  sync_check：dev 副本 vs 生产副本 diff 校验           │
├─ 测试层（每个 bug 一个守护测试）────────────────────┤
│  单测：pytest（函数级）                              │
│  效果测试：mcp_smoke.py（真实 stdio 协议，48 工具）   │
│  状态效果测试：生产 state 文件前后字节不变断言         │
│  契约测试：_TOOLS 清单、_DEFS_CACHE 稳定             │
└──────────────────────────────────────────────────┘
```

**层级原则**：上层失败必须能指出下层哪个检查会抓住它。
pytest 抓不住的（协议层/异步/进程级），必须有一层效果测试兜底——**禁止"pytest 全绿就发布"**。

---

## 3. P0-P2 落地清单（unified-rx-mcp）

### P0（本次交付后一周内，最高优先）

- [ ] **P0-1 mcp_smoke 入 CI**：`mcp_smoke.py` 的 `SERVER` 改相对路径解析（当前 `L13` 是绝对路径，CI 不可移植）；ci.yml 加一步 `python scripts/mcp_smoke.py`
- [ ] **P0-2 tool_ratchet 基线**：生成 `tools.json`（28 核心 + 20 扩展 = 48），CI 校验 `_definitions()` 与基线一致；README/plugin 工具数改从基线生成（或 CI 断言三处一致）
- [ ] **P0-3 actionlint 硬门禁**：`github-action-security.yml` 去掉两处 `continue-on-error: true`（zizmor 已是硬门禁）

### P1（两周内）

- [ ] **P1-1 state 效果测试**：pytest 增加"测试前后 `~/.unified-rx/lse-state.json` 字节不变"断言（防 fixture 被删）
- [ ] **P1-2 `_DEFS_CACHE` 契约测试**：同进程两次 `_definitions()` 结果逐字节一致 + 扩展构建只走 async 路径
- [ ] **P1-3 async_guard**：脚本扫描 server.py，断言同步路径（`_definitions`/`_call`/`_call_ext`）不出现 `asyncio.run` 调用（直接 grep + 调用链检查）

### P2（择机）

- [ ] **P2-1 sync_check**：dev vs `E:\共享\51` 生产副本 12+ 文件 diff 校验脚本（当前人工 diff）
- [ ] **P2-2 wf_check 本地预检**：workflow 改动本地跑 zizmor/actionlint/REUSE（现在靠 CI）
- [ ] **P2-3 vuln-scan 入 pre-push**：本地四连脚本固化

---

## 4. 铁律（写给未来的 agent）

1. **修复 = 代码 + 守护测试**。提交信息必须能回答："哪个检查能抓住这个 bug 的回归？"答不上来就补一个再提交。
2. **pytest 全绿不等于系统没坏**。协议层/进程级/异步行为，用 `scripts/mcp_smoke.py` 真实启动验证——它是 `internal/boot/effect_test.go` 在 Python 侧的对应物。
3. **数字声明必须可生成或可校验**。README/plugin 里的工具数、行数、测试数，要么从单一来源生成，要么 CI 断言一致——人肉维护的数字必然漂移。
4. **软门禁等于没门禁**。`continue-on-error: true` 只用于"信息收集"类步骤；任何"应该永远绿"的检查必须是硬门禁。
5. **双副本必须同步**。dev 仓库与生产副本（`E:\共享\51\unified-rx`）diff 是提交的一部分；`sync_check` 脚本化之前，提交信息里写清同步清单。
6. **棘轮只松不紧**。基线文件记录债务可以，但只允许在"重命名/提取"时用 `-update` 扩大，且 PR 里必须解释——与 `tools/repolint -update` 同规则。

---

## 5. 状态

- 初稿：2026-08-11（unified-rx-mcp 四大 bug 修复后）
- 已闭环：asyncio.run 崩溃（mcp_smoke.py 已建）、state 污染（LSE_STATE 隔离已建）、工具数 48 一致（README/plugin/冒烟三方一致）
- 待落地：P0-1/P0-2/P0-3 三项（见第 3 节勾选清单）

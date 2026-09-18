# Reasonix 发布

<a href="./RELEASING.md">English</a>

Reasonix 只有一条面向用户的发布线：正式的 `X.Y.Z` 版本。发布引擎沿用经过验证的
Stable 发布拓扑：同一个 `main-v2` 提交上的三个不可变 Git 标签，外加一个受保护的编排器。

| 发布面 | 不可变标签 | 公开结果 |
| --- | --- | --- |
| CLI | `vX.Y.Z` | GitHub Release 与 Homebrew |
| npm | `npm-vX.Y.Z` | 根包与各平台包；`latest`、`canary`、`next` 兼容别名 |
| Desktop | `desktop-vX.Y.Z` | 已签名 GitHub Release、不可变 R2 目录以及 `latest/latest.json` |

这三个标签是实现身份标识，不是用户可选的渠道。它们必须始终解析到同一个提交，
且永远不得移动或删除。

Go SDK 模块（`sdk/go`）与三个产品发布面独立版本化：其标签形如 `sdk/go/vX.Y.Z`
（首个为 `sdk/go/v1.0.0`），指向首次发布相应 Extension Protocol 主版本的发布提交。
SDK 标签不会触发产品发布，不会移动，也不会改变上述三标签契约。

## 日常发布流程

常规开发者路径只有：一个版本输入、一个经过评审的 Notes PR、一条终端命令和一次环境审批：

1. 打开 Actions → **Prepare release**，输入 `X.Y.Z`。
2. 评审并合并生成的双语 release-notes PR。如果它的检查显示 `action_required` 且没有任何
   job，说明该 PR 由 Actions bot 打开，而用 `GITHUB_TOKEN` 触发的事件永远不会启动
   workflow；运行 `./scripts/release-notes-pr-kick.sh X.Y.Z` 用你自己的凭据关闭并重新
   打开它，CI 才会启动。该脚本会拒绝其他任何零检查形态。
3. 在已认证的维护者检出中运行：

   ```sh
   ./scripts/release-stable.sh X.Y.Z
   ```

4. 在 `release` 环境中批准由此产生的 **Release stable** 运行一次。
5. 等待其 postflight 验证 CLI、npm、Desktop、R2、Homebrew 和 changelog。

如果仓库策略阻止 Actions 打开 Notes PR，workflow 仍会推送 `release-notes/vX.Y.Z`，
并打印以下可恢复的交接命令：

```sh
gh pr create --repo esengine/DeepSeek-Reasonix \
  --base main-v2 --head release-notes/vX.Y.Z --fill
```

不要仅仅因为 PR 创建被拒就重新生成 Notes。

## 标签助手证明什么

`scripts/release-stable.sh` 在创建任何公开引用之前即会失败，除非：

- 版本号是规范的 `MAJOR.MINOR.PATCH`；
- 远程 `main-v2` 是引入或更新完整、已评审 Stable 目录记录的那个提交；
- 精确提交的 `main-v2` CI 已成功完成。只改动 `release-notes/` 的提交按设计会跳过
  代码矩阵，因此对这类候选，助手还要求最近一个改动了其他内容的 first-parent 祖先
  （最多五跳）的 push CI 为绿色；
- `vX.Y.Z`、`npm-vX.Y.Z` 和 `desktop-vX.Y.Z` 均不存在。

随后它用一次原子 Git 事务，为该 `main-v2` SHA 推送一个 no-op guard 以及全部三个
轻量标签。如果 CI 运行期间 `main-v2` 前进了，整个事务会被拒绝，且不会消耗任何
版本标签。因此部分标签集不是正常的失败形态。`vX.Y.Z` 事件会启动既有的受保护
Stable relay；维护者无需手动派发 CLI、npm 或 Desktop 发布器。

## 发布与审批

受保护的 Stable workflow 会把三个标签重新解析到 `main-v2` 历史上的同一个 SHA，
重新验证候选提交引入了已评审的 Notes 且通过精确 SHA 的 push CI，并在请求唯一的
人工审批之前运行缓存守卫。原子标签事务之后 `main-v2` 可以安全前进，不会使该候选
失效。审批之后它会先执行不发布的 SignPath preflight，再针对不可变候选运行 CLI、
npm 和 Desktop 发布器。

npm 发布器把 `latest`、`canary` 和 `next` 推进到同一个正式版本。`canary` 和 `next`
仅保留以兼容历史脚本继续安装受支持的构建；它们不是测试渠道，也不对外宣传。

无需自定义 GitHub App、App 私钥、仓库所有者设置变更、手动标签界面或子 workflow 审批。

## 恢复

对于部分完成的 Stable 发布，在受保护的 `main-v2` 上打开 **Release stable**，输入
已有的 `vX.Y.Z`，只选择缺失的发布面，并批准一次 `release`。恢复只接受仍位于
`main-v2` 历史中的不可变三标签集。它必须复用匹配的公开内容，并在校验和、签名、
manifest、npm provenance 或 R2 对象冲突时 fail closed。

永远不要移动、删除或重建已发布的标签。产品修正以更高的 patch 版本发布。

## 已退役的预发布路径

常规 Preview、Canary 和 RC 发布入口已禁用。历史标签、Release、包版本、changelog
页面和最终桥接端点仍保留可用以兼容，但不再出现在当前的下载导航或发布准备中。

旧 CLI 和 Desktop 的渠道设置解析到正式发布线。冻结的 Preview 端点继续把旧客户端
引导到桥接构建，进而升级到当前正式版本。

## 切换后的首次发布

本次变更后的首次发布，需独立验证：

- 三个标签解析到已评审 Notes 的合并 SHA；
- 两个 GitHub Release 都包含其完整且符合预期的资产；
- npm 根包与全部六个平台包都报告该 SHA，且 `latest == canary == next`；
- R2 不可变与 latest manifest 逐字节一致，且每个 URL 均可用；
- Homebrew 与 reasonix.io 显示同一版本；
- 旧桥接客户端能升级到正式版本；
- v1.38.3 或更早的桌面端报告
  `unsupported install_layout "electron-v1" (keeping current version)`，且其安装保持
  原样；reasonix.io 的 `#start` 小节为这些客户端显示手动整包安装提示。

在所有公开发布面都达到终态且已验证之前，发布不算完成。

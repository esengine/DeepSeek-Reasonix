# 原生 Windows 全量测试摘要

- 验证日期：2026-08-10
- 代码基线：`9f22a74f1fbc657d534e8536c092dc8d96165a3c`
- GitHub Actions：[Windows Native Full #31401828846](https://github.com/CharlesReveries/DeepSeek-Reasonix/actions/runs/31401828846)
- Runner：`windows-latest`
- Shell：Git for Windows Bash（工作流显式拒绝 WSL）
- 总耗时：15 分 25 秒
- 最终结论：通过

## 模块结论

| 门禁 | 结论 | 证据 |
| --- | --- | --- |
| 原生 Windows 工具链 | 通过 | Git for Windows Bash 路径校验通过，WSL 拒绝检查通过 |
| Root Reasonix | 通过 | `go test -p 4 -count=1 -timeout=8m ./...` |
| Go SDK | 通过 | `go test -count=1 -timeout=8m ./...` |
| Desktop 前端准备 | 通过 | Wails 代码生成、pnpm 锁定安装、前端构建与性能预算均通过 |
| Desktop | 通过 | `go test -p 1 -count=1 -timeout=12m ./...` |
| 日志上传 | 通过 | `windows-native-go-tests-31401828846`，保留 14 天 |
| 聚合门禁 | 通过 | Root、SDK、Desktop prep、Desktop 均为 `success` |

## 日志复核

下载并复核 `root.log`、`sdk-go.log`、`desktop-prep.log` 与 `desktop.log`：未发现 `FAIL`、panic、fatal error 或 timeout 标记。关键尾部结果如下：

- Root：测试包以 `ok` 结束；无测试文件的包以 `[no test files]` 结束。
- SDK：主 SDK 与 starter extension 示例测试通过。
- Desktop：主程序及签名、更新、Windows resource、卸载相关包测试通过。
- Desktop frontend：初始 JavaScript、最大 chunk、CSS 与本地化 chunk 的 gzip/raw 性能预算全部通过。

本摘要记录产品代码基线的首次成功运行。提交本摘要后产生的最终重复验证链接记录在对应草稿 PR 中，以避免为更新 CI 链接持续制造新的证据提交。

## 重复验证说明

证据提交触发的第二次运行 [#31403367840](https://github.com/CharlesReveries/DeepSeek-Reasonix/actions/runs/31403367840) 再次通过 Root、SDK、Desktop 前端准备及绝大多数 Desktop 测试，但暴露 `TestAutosaveFailureRetriesAndRecoversOnNextTurnDone` 的 Windows 时序假失败：测试等待仅 2 秒，短于生产元数据锁单次 5 秒等待窗口。分支随后只扩展该故障恢复断言的等待范围，普通自动保存测试仍保留 2 秒门槛；最终修复验证以草稿 PR 中记录的最新 run 为准。

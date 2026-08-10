# 上游 Reasonix Go 验证报告

- 日期：2026-08-10
- 工具链：Go 1.26.5 windows/amd64
- 来源：[Go 官方发布包](https://go.dev/dl/go1.26.5.windows-amd64.zip)
- SHA-256：`97e6b2a833b6d89f9ff17d25419ac0a7e3b482a044e9ab18cdef834bd834fd38`（与 Go 官方发布清单一致）

## 结果

| 范围 | 命令 | 结果 |
| --- | --- | --- |
| 根 Reasonix 模块全包编译 | `GOFLAGS=-buildvcs=false go test -p 4 -run '^$' ./...` | PASS |
| Go SDK 完整测试 | `cd sdk/go; go test ./...` | PASS |
| Desktop 全包编译 | `cd desktop; GOFLAGS=-buildvcs=false go test -run '^$' ./...` | PASS |
| 根 Reasonix 模块完整测试 | `go test -p 4 -timeout=3m ./...` | 已执行，环境门禁未通过 |
| Desktop 完整测试 | `cd desktop; go test ./...` | 已执行，环境门禁未通过 |

## 完整测试未通过的环境原因

1. 当前 Windows 会拒绝临时 Go 测试可执行文件连接自己刚监听的 `127.0.0.1` 端口。已用只包含 `httptest.NewServer` 和 `server.Client().Get` 的最小程序独立复现；显式 `NO_PROXY` 后仍失败。这会影响 billing、web fetch、provider model、telemetry、updater 和本地服务测试。
2. WinINET 与 WinHTTP 均配置本机 `127.0.0.1:10809` 代理。Reasonix 能读取该系统配置，但即使完全绕过代理，上一条 loopback 限制仍存在。
3. WSL 已安装但没有可用 `/bin/bash`，因此 Windows 打包与部分 e2ebench 用例无法执行其 Bash 脚本。
4. `REASONIX_RELEASE_CACHE_GUARD=1` 的真实 DeepSeek 缓存门禁返回 HTTP 503，随后 `internal/agent` 与 `internal/boot` 达到每包 3 分钟超时。
5. Desktop 的三条快速工作区切换用例在当前机器上未能及时结束 controller，并留下短时 `.lease.lock` 文件锁。

## 结论与复验要求

上游 Go 测试不再是“未安装 Go、未执行”的未知状态：工具链已安装，三个模块均已实际运行，且所有包可以编译。完整测试仍需在允许临时测试进程 loopback、具备 Bash 的干净 CI runner 上复验；真实 DeepSeek 缓存门禁应在供应商服务稳定时单独执行。上述环境问题不影响本次 intelifar Web UI 的 Node、浏览器和真实端口 E2E 全绿结果，但不能把上游 Go 完整测试标记为 PASS。

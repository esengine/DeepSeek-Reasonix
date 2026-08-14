# Windows 原生 Go 测试安全事件记录

## 结论状态

本轮全量 Go 测试已停止，不能记为通过。告警由 360 HIPS 触发，目标是 `go test` 为 `reasonix/internal/ablation` 生成的临时测试程序。后续 360 扫描报告为“发现安全威胁 0 / 未发现安全威胁”，但在取得安全厂商复核前，本报告不把该事件直接定性为误报，也不把临时程序恢复或加入目录白名单。

截至 2026-08-10 21:45（Asia/Shanghai）：

- `go.exe`、`compile.exe`、`link.exe` 相关测试/构建进程：0；
- `intelifar native Go tests` 临时防火墙规则：0；
- Root、SDK、Desktop 全量测试：未完成；
- WSL、Docker：未使用；
- Windows Sandbox：当前未启用；
- 工作区源代码对象：`git fsck --no-dangling` 通过。

## 本地检测证据

360 HIPS 事件日志：

`C:\Users\thuhc\AppData\Roaming\360Safe\360ScanLog\360Tray.exe.ESG.2026-08-10.log`

- 时间：2026-08-10 21:38:48；
- 目标：`C:\Users\thuhc\AppData\Local\Temp\go-build3132163158\b628\ablation.test.exe`；
- 360 记录 MD5：`9002395a362002f786c21656836c5e26`。

360 后续扫描报告：

`C:\Users\thuhc\AppData\Roaming\360Safe\360ScanLog\ScanLog_2026-08-10_21_40_16.txt`

- 扫描结果为未发现安全威胁；
- 该精确临时路径/MD5 出现在报告的白名单设置段；
- `ablation.test.exe` 当前已不存在，未由本任务恢复；
- 本任务没有添加杀毒目录白名单、关闭安全软件或上传样本。

该测试程序对应源码为 `internal/ablation/ablation.go` 与 `internal/ablation/ablation_test.go`，功能是解析基准测试的子系统开关，不包含网络、进程启动、持久化或文件写入逻辑。

## 工具链证据

Go 官方 Windows amd64 压缩包：

`C:\Users\thuhc\AppData\Local\Codex\toolchains\downloads\go1.26.5.windows-amd64.zip`

SHA-256：`97E6B2A833B6D89F9FF17D25419AC0A7E3B482A044E9AB18CDEF834BD834FD38`

该值与下载时校验的 Go 官方清单一致。测试入口还设置 `GOTOOLCHAIN=local`，不会在运行时自动下载另一套 Go 工具链。

## 已删除且从未执行的临时文件

下列文件是中止测试后残留的未签名测试二进制。未签名是 `go test` 临时产物的正常属性，但不能单独证明安全；本任务仅计算哈希，没有运行文件。用户确认继续安全处置后，两个 EXE、两个辅助文件和 748 个空构建目录已删除，`go-build3132163158` 根目录不再存在：

| 文件 | SHA-256 |
| --- | --- |
| `...\go-build3132163158\b635\agent.test.exe` | `95723EC65422221579C070B90778A0A784B895EC168502E534BCC96133494318` |
| `...\go-build3132163158\b793\openai.test.exe` | `F9B4B422B678721A667696844BDAB74F8BDA3FCEE5ED0A4CB2A8CC6AAE67DF6E` |

## 后续安全路径

1. 推荐：由用户通过 360 官方软件误报反馈渠道提交告警截图、精确文件说明和可复现样本，等待厂商结论；提交文件属于外部数据传输，本任务未代为执行。
2. 在厂商结论前需要继续测试时，使用 GitHub Actions 的 `windows-latest` 原生 runner 或独立 Windows VM；仓库现有 CI 已覆盖 Root Windows、SDK Windows 与 Desktop Windows，不依赖 WSL。
3. 不建议：关闭 360、加入整个 Go/Temp/仓库目录白名单，或在宿主机直接重跑全量测试。这些做法会扩大信任面并掩盖真实安全事件。
4. 两个残留临时二进制已在记录哈希且获得用户确认后删除，不可恢复。

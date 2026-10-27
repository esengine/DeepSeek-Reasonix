---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-27
---

# Published command Job Object owner / 已发布命令的 Job Object 所有权

## Behavior / 行为

Repeated cancellation of a tracked Windows command keeps the same published
`TrackedJob`. Its one-shot release closes the Job Object once; later cancellation
uses the original Go process object instead of invoking PID-based `taskkill`.
A successfully assigned Job Object also replaces the auxiliary PID tree walker.

Windows 跟踪命令重复取消时保留同一个已发布 `TrackedJob`。Job Object 只关闭一次；
后续取消使用原 Go 进程对象，不再因丢失 owner 而调用按 PID 寻址的 `taskkill`。
成功分配 Job Object 后，也不再启动额外的 PID 树扫描器。

The shared owner is `proc.RunCommand`, used by the shell tool. Cancellation,
bounded cleanup retries and shell reaping all reach `TrackedCommand.Kill`.

Keeping the released guard matters after `cmd.Wait`: a PID can eventually be
reused after its process object is released. See Microsoft's
[process identifier lifetime explanation](https://devblogs.microsoft.com/oldnewthing/20110107-00/?p=11803).

共享 owner 是 shell 工具调用的 `proc.RunCommand`。取消、有限重试清理和 shell
收尾都会调用 `TrackedCommand.Kill`。`cmd.Wait` 后仍须保留已释放的 guard：进程
对象释放后，PID 最终可能被重用；不能把数字继续当作原命令身份。

## Mechanism boundary / 机制边界

| Route | Contract / 约定 |
| --- | --- |
| Published Windows Job Object | Keep its one-shot owner; no auxiliary PID walker / 保留一次性 owner，不启动额外 PID 扫描 |
| Late published Job Object after cancellation | Retain and release that same owner / 保存并释放同一 owner |
| Unix process group or unavailable Windows Job Object | Existing fallback and retry behavior remains / 沿用原回退与重试 |
| Untracked command and direct KillTree | Existing PID-based Windows fallback remains / 原按 PID 回退仍在 |
| Standalone TreeTracker | Generation refresh, identity checks and termination handles need separate work / 代际刷新、身份检查与终止句柄另需闭环 |
| Launch failure before owner publication | Existing cleanup remains / 沿用原启动失败清理 |

This does not establish complete descendant quiescence or a recovery receipt.
Unknown execution scope still needs retention and diagnosis. No permission,
configuration, API, persisted format, cancellation budget, model request or
cache-stable prefix changes. There is no demonstrated PID-reuse incident here.

本修改不证明后代进程完全静止，也不产生恢复凭证。执行范围未知时仍须保留资源并
诊断。不改权限、配置、API、持久化格式、取消预算、模型请求或缓存稳定前缀。
这里没有已复现的 PID 重用或误杀事故。

## Verification / 验证

Windows fixtures launch only the test's own helper process through `RunCommand`.
A renamed test executable records `taskkill` arguments without terminating any
PID.

After normal or canceled waiting returns the published owner, the probe is
reset and repeated reaping must leave no marker. Stop attempts before that
returned-owner boundary are outside this assertion. An explicit legacy fallback
control must create a marker.

Windows 回归只通过 `RunCommand` 启动测试自有子进程。重命名的测试程序只记录
`taskkill` 参数，不终止任何 PID。正常或取消等待返回已发布 owner 后重置探针，
重复收尾不得产生标记；该返回边界之前的停止尝试不在此断言范围。显式旧回退对照
必须产生标记。

The dedicated Windows gate runs unchanged fixtures against the new owner and an
overlay reversing the ownership changes. It records the reconstructed SHA-256.

For this unit that digest must match the exact preceding owner,
`61b4e1f88e32322fa287b9d1ae71edeb0cd7f2511e0894cbd811d49794590c3a`,
before qualifying its native counterfactual evidence.

Later unrelated owner edits remain in the reconstruction. Its digest then
describes an ownership counterfactual, not this complete historical file.

It requires seven named
pass events for the new owner, four expected fail events and three passing controls
for the preceding owner, plus each package terminal.

Parent and helper events are
included; these are not seven independent cases. Cross-compilation alone does not
qualify this native behavior.

The recorder identifies its executable path, not argv[0], because the bare
`taskkill` command can omit the executable suffix in that argument. A dedicated
alias probe uses only the inert helper-test selector if recorder detection fails.

The fixture requires LookPath to select that same file. The alias probe and
KillTree use synchronous Run/Wait before temporary-directory cleanup. Unexpected
recorder entry fails closed instead of starting the complete package suite.

记录替身检查实际可执行文件路径，不依赖 argv[0]；裸 `taskkill` 命令的该参数可能
不含扩展名。专用别名对照在入口误判时只选无副作用的辅助测试，避免递归运行整包。
Fixture 断言 LookPath 解析到同一文件；别名探针和 KillTree 同步等待 Run/Wait
完成后才清理临时目录。异常替身入口立即失败，不启动完整包测试。

专用 Windows 门禁运行新 owner 与反转所有权修改的 overlay，记录重建文件 SHA-256，
测试保持相同。本单元原生反例资格还须核对它等于上述精确旧 owner 的摘要。
以后无关 owner 修改保留在重建文件中；届时它是所有权反例，不冒称完整历史文件。

新版须有七个具名通过事件；反例须有四个预期失败事件和三个通过对照，且均
核对包终态。计数包含父测试和辅助测试，不是七个独立用例；交叉编译不能替代原生
行为验证。

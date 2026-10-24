# Product concurrency qualification / 产品并发验收

Run real processes through `boot.Build`, Controller admission, session leases,
file/Bash tools and final persistence. Only the provider is simulated. Each
worker runs a real fixture `go test -count=1 ./...` that checks its output.
The parent independently verifies every expected file.

Every completed round is saved atomically. Preparation and execution failures
retain worker stderr and exit status in the report. Partial reports remain
`completed=false` and the analyzer rejects them as qualification results.

使用真实进程执行 `boot.Build`、Controller 准入、会话租约、文件与 Bash 工具及
最终持久化。仅模型由固定脚本模拟。每个 worker 运行真实的 Go 测试检查自身
输出，父进程再独立核对全部文件。

每轮结果原子保存；准备或执行失败时保留 worker 的 stderr 和退出状态。
未完成的报告保持 `completed=false`，分析器拒绝将其作为完整验收结果。

```sh
go build -o /tmp/reasonix-product-concurrency ./benchmarks/product-concurrency
/tmp/reasonix-product-concurrency -trials 20 -workers 4 -steps 3 \
  -think 100ms -output product-throughput.json
python3 benchmarks/product-concurrency/summarize.py product-throughput.json \
  --output product-throughput-summary.json
/tmp/reasonix-product-concurrency -trials 3 -workers 4 -steps 3 \
  -think 100ms -record-locks -output product-locks.json
python3 benchmarks/product-concurrency/summarize.py --self-test product-locks.json \
  --output product-locks-summary.json
python3 benchmarks/product-concurrency/cancel-probe.py /tmp/reasonix-product-concurrency
python3 benchmarks/product-concurrency/failure-probe.py /tmp/reasonix-product-concurrency
```

The failure probe uses POSIX argv injection to make a real assembled worker
reject an invalid policy. It checks retained stderr, exit status and the
incomplete report without changing the public benchmark protocol.

失败复现使用 POSIX argv 注入，让真实装配的 worker 拒绝无效策略，
验证 stderr、退出状态和未完成报告，不修改基准的公开协议。

Keep throughput and trace runs separate. Record source and executable hashes,
CPU count, platform and concurrent machine load with the raw reports. Add a
`-think 500ms` run for longer inference. Each pair alternates mode order and uses
a fresh disposable home, cache and workspace.

Coordination retains the account-wide protocol directory. Resource identities
come from each disposable workspace; user configuration and credentials are not
part of the workload.

吞吐量和锁采样分开执行，保留原始报告、源码与二进制摘要、CPU 数量、平台及
同期负载。另用 `-think 500ms` 测量长推理。每组交替模式顺序，并使用全新的
一次性 home、cache 和工作区，不使用个人配置或凭证。

协调目录沿用账号级协议，各轮资源标识来自一次性工作区。

| Workload / 负载 | Actual tools / 实际工具 | Integrity check / 完整性检查 |
| --- | --- | --- |
| named | `write_file` to distinct files / 写入独立文件 | Exact bytes per step / 逐步字节核对 |
| opaque | Bash append / Bash 追加 | No lost or duplicate line / 无丢行或重复 |
| same-file | `read_file`, `edit_file` CAS / 同文件比较编辑 | Every final marker / 全部最终标记 |

The baseline uses this binary's legacy policy with no collaboration field.
The comparison explicitly selects `shared`. Workers check the assembled policy.
Same-file version conflicts trigger a new read and attempt; other failures abort.

基线为同一二进制的旧兼容策略，不保存 collaboration；对照明确选择 `shared`。
worker 检查实际装配策略。同文件版本冲突后重新读取及重调度，其他失败直接终止。

Both modes first execute the same Bash write of a per-worker admission marker.
The legacy policy can acquire its workspace scope before holding narrower claims;
otherwise parallel file claims followed by the verification command's scope
expansion can refuse each other. This is a completing-workload comparison;
retain separate failed runs as evidence of that legacy limitation.

两种模式首先执行相同的 Bash，写入各 worker 的准入标记，让旧策略在持有较窄
文件预约前申请工作区范围。否则，并行文件预约随后扩大到验证命令范围可能
互相拒绝。这是可完成负载的性能对照；旧模式失败的运行记录另外保留。

Wall starts after all Controllers are built, leased and ready. It includes turns,
snapshots, shutdown and process exit. Preparation is separate. Median averages
the middle pair; P95 uses nearest rank. Conflicts count only actual typed
`workspace.mutation_version_conflict` results.

墙钟从全部 Controller 构建、绑定租约并就绪开始，包括回合、快照、关闭及进程
退出；准备时间单独报告。中位数取中间两值均值，P95 使用最近秩。
冲突数只统计实际返回版本冲突错误码的工具结果。

OS hold lower bounds span lock success to before unlock; upper bounds include
the attempt and descriptor close. Claims and exclusive workspace locks are
resource holds. Registry, preparation, liveness and session locks remain
identifiable in raw traces.

OS 持锁下界从加锁成功到解锁前，上界包括申请和关闭描述符。claim 和工作区
独占锁计入资源持锁；注册、准备、活性及会话锁在原始采样中分别保留。

Intervals merge within a worker, then sum across workers and may exceed wall.
`tool_dispatch_to_result` includes admission and waiting; it is not a pure
filesystem execution timer. The analyzer rejects shared resource holds
overlapping simulated provider inference.

Throughput summaries omit OS bounds when recording is disabled. Missing samples
are not reported as zero holds.

区间先在 worker 内合并，再跨 worker 求和，可能超过墙钟。工具 dispatch 到
result 包含准入及等待，不等于纯文件执行耗时。分析器拒绝新版资源持锁与
模拟模型推理重叠。

关闭 OS 采样时，吞吐量摘要不提供持锁边界，不能把缺失的采样显示为零持锁。

`notified_resource_wait` measures transaction waiting notices. Legacy waiting
has no equivalent event identity and is omitted. Unnotified waiting remains
included in dispatch-to-result. Summary schema 2 states this metric scope.

`notified_resource_wait` 只计事务等待通知。旧模式没有对应事件身份，摘要不提供
该指标。未通知的等待仍包含在 dispatch 到 result 区间内。摘要版本 2 明示此范围。

This fixture covers independent shared sessions. Isolation, fleet, recovery,
frontend interaction and controlled verification need separate evidence.
Ordinary fixture tests do not mint controlled verification receipts.

此基准覆盖独立会话共享协作；隔离、fleet、恢复、前端交互和受控验证分别验收。
普通 fixture 测试通过不产生受控验证凭据。

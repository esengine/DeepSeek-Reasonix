---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Coordination performance evidence / 协调器性能证据

This measures the coordination owner. It does not qualify complete task,
Build, UI, isolation preparation or integration performance, or enable defaults.

本报告测量协调器所有者，不能替代完整任务、Build、界面、隔离准备及整合性能验收，
也不授予开启新默认策略的资格。

## Method / 方法

The same Linux ARM64 Lima guest ran alternating paired modes, with Go 1.26.5,
two logical CPUs and 3 GiB RAM. Each scope used 20 pairs, four child processes,
three writes per process and 2 ms simulated execution per write.

同一 Linux ARM64 Lima 虚拟机交替运行配对模式：Go 1.26.5、2 个逻辑 CPU、3 GiB
内存。每种范围 20 组、4 个真实子进程、每进程 3 次写入，每次模拟执行 2 ms。

| Scope / 范围 | Coordinated resource / 协调资源 |
| --- | --- |
| named | Independent files / 不相交文件 |
| opaque | Workspace per operation / 每次操作占用工作区 |
| same-file | One file, transaction CAS retry / 同文件，事务版本比较及重调度 |

Wall time starts when every worker is ready and ends after all exit. Startup is
excluded. Acquisition includes admission and version checks. Legacy retains its
claim across simulated inference; transactions release before inference.

总耗时从所有工作者就绪至全部退出，不含启动。申请时间包含准入及版本校验。
旧模式在模拟推理期间持有 claim；事务模式在推理前释放。

No other qualification ran inside the guest during measurement. The host ran
Go tests and a Windows VM; these results do not establish an idle physical host.

测量期间 guest 内没有其他验收重叠；宿主同时运行 Go 测试及 Windows VM，不能称为
物理机完全空闲。guest 一分钟负载范围：20 ms 组 0.168–0.499；100 ms 组
0.253–0.895；500 ms 组 0.002–0.253；轨迹组 0.049–0.058。

## Wall time / 总耗时

Milliseconds; each cell is median / nearest-rank P95. Conflict counts cover all
20 transaction trials, not a single trial. Each CAS conflict was rescheduled.

单位 ms，每格为中位数 / 最近秩 P95。冲突数统计全部 20 次事务试验，每次 CAS
冲突均重调度，不代表单次试验的冲突数。

| Inference / 推理 | Scope | Legacy | Transaction | Conflicts / reschedules |
| --- | --- | --- | --- | --- |
| 20 ms | named | 319.47 / 402.40 | 241.46 / 305.68 | 0 / 0 |
| 20 ms | opaque | 553.68 / 626.03 | 305.78 / 428.32 | 0 / 0 |
| 20 ms | same-file | 552.95 / 567.59 | 527.64 / 721.76 | 429 / 429 |
| 100 ms | named | 560.37 / 611.68 | 410.30 / 749.66 | 0 / 0 |
| 100 ms | opaque | 1491.29 / 1520.96 | 409.91 / 586.63 | 0 / 0 |
| 100 ms | same-file | 1494.30 / 1514.63 | 446.30 / 851.24 | 137 / 137 |
| 500 ms | named | 1766.91 / 1778.35 | 1596.75 / 1617.48 | 0 / 0 |
| 500 ms | opaque | 6246.81 / 6317.16 | 1628.88 / 1759.16 | 0 / 0 |
| 500 ms | same-file | 6266.76 / 6321.61 | 1596.19 / 1667.20 | 74 / 74 |

All trials retained every expected write. Transaction medians improved in this
load, but 20 ms same-file and 100 ms named P95 regressed. This is not evidence
that every workload is faster or that high-contention CAS retries are free.

全部试验保留所有预期写入。该负载下事务模式中位数降低，但 20 ms 同文件和
100 ms 不相交文件的 P95 回退，不能据此宣称所有场景更快，或高竞争重试没有成本。

## OS-lock trace / OS 锁轨迹

Three additional pairs per scope used 100 ms inference with in-memory syscall
boundaries. Values below are medians, in milliseconds, summed across workers.
Concurrent spans may exceed wall time; columns are not additive partitions.

每个范围另运行 3 组、推理 100 ms，内存记录锁系统调用边界。下表为 ms 中位数，
跨工作者累加；并发区间可能超过总耗时，各列不能直接相加。

| Scope / mode | Resource hold lower–upper | Execution | Resource wait | Registry hold | Preparation | Inference overlapping resource |
| --- | --- | --- | --- | --- | --- | --- |
| named / legacy | 1297.041–1297.073 | 54.544 | 475.499 | 7.432 | 0 | 1236.205 |
| named / transaction | 74.436–74.490 | 44.140 | 0 | 47.707 | 30.145 | 0 |
| opaque / legacy | 1299.771–1299.806 | 57.910 | 2327.636 | 9.616 | 0 | 1233.091 |
| opaque / transaction | 67.159–67.203 | 43.596 | 0 | 37.235 | 25.230 | 0 |
| same-file / legacy | 1293.835–1293.869 | 50.879 | 2322.273 | 9.498 | 0 | 1234.781 |
| same-file / transaction | 64.576–64.622 | 43.032 | 0 | 37.541 | 22.297 | 0 |

Resource bounds union exclusive workspace/claim locks per worker, including
stale-record probes. Lower bounds start after lock success and end before
unlock; upper bounds include those syscall windows.

资源区间在每个工作者内合并独占 workspace/claim 锁，包含旧记录探测。下界从加锁
成功后到解锁前；上界额外包含这些系统调用窗口。

Resource wait counts blocked resource-lock time, excluding preparation,
registry admission and version checks. Zero does not mean zero acquisition cost.
Overflow, unclosed holds and transaction resource overlap with inference fail
the traced run. All traced runs passed these checks.

资源等待仅统计资源锁阻塞，不含准备、注册准入及版本校验；零不表示申请没有开销。
轨迹溢出、未闭合持锁、事务资源锁与推理重叠均使试验失败，本轮全部通过。

## Reproduction and provenance / 复现及身份

Build `./benchmarks/mutation-coordination`. Run with `-trials 20 -workers 4
-steps 3 -think 20ms -write 2ms -output result.json`; repeat for 100 ms and
500 ms. Trace separately with `-trials 3 -think 100ms -record-locks`.

构建及运行参数如上。性能模式和轨迹模式分开测量；后续生产变更需要重新确定其
影响范围，不能把这份历史结果自动视为最新整套产品的性能验收。

| Artifact / 产物 | SHA-256 |
| --- | --- |
| nested-dependencies-v1-source.tar.gz | e17a8ed8eeddbc5c7c8eab6f339ee6a16e5a639902b98d98c6e53dab2af83869 |
| Linux benchmark binary | 6476d68b1fe5dc72deccc598779a91b30ad9c72eeb2b103ea874142b6859cb64 |

The qualification bundle retains raw JSON, lock/phase traces, load samples,
commands and binary build identity under `coordination-nested-v1/` in
`linux-nested-dependencies-v1-results.tar.gz`.

验收包保存原始 JSON、锁及阶段轨迹、负载采样、命令及二进制构建身份。此前高负载
macOS 测量是另一组证据，不能跨平台当作本报告的同机基线。

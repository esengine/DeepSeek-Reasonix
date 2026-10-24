# Coordination measurements / 协调机制测量

These measurements use real processes and files with simulated inference.
They qualify the coordination owner; Build, recovery, native-platform and product
acceptance require their own evidence. Existing installation defaults remain gated.

本记录使用真实进程与文件，模拟推理耗时。它测量协调 owner；真实装配、恢复、
原生平台和产品验收须分别记录。新安装默认策略仍受完整验收门槛约束。

## Workload / 负载

Each scope uses four processes, three writes per process and 2 ms simulated active
operation time. Twenty paired trials alternate legacy/transaction order. Every
round verifies each expected append occurs exactly once.

每范围使用四个进程、每进程三次写入、每次模拟操作 2 ms。二十组同机对照交替
执行旧模式和短事务，逐轮检查无丢失、重复或意外内容。

The macOS ARM64 host has ten logical CPUs. Its observed one-minute load ranged
approximately 94–206; platform VMs and tests overlapped these measurements.
This is a measured loaded-host comparison, not an idle-machine baseline.

macOS ARM64 主机为十逻辑核，观测到的一分钟负载约为 94–206，期间有虚拟机与
其他验收测试。以下数据属于带载同机对照，不能宣称为空闲机器基准。

Source archive SHA256:
`ab813eb38b1749531ea8d0abcd2ff8d88042f97d0fbe1c7944e66e724e1d714b`.

Executable SHA256:
`534e5873f1d6ecabffb011df1a3ea568d13ea9232266d286b93f61dbf8167980`.
A separate rebuild produced the same executable hash.

## Throughput without recording / 无采样耗时

Wall starts after every worker is ready and ends after every worker exits.
Median averages the middle pair for twenty samples; P95 uses nearest rank.
Conflicts and reschedules count actual CAS retries across 240 writes per row.

墙钟从全部 worker 就绪后开始，至全部退出结束。中位数取中间两值均值；P95
采用最近秩。每行共 240 次写入，冲突及重调度数来自实际 CAS 重试。

| Think / 推理 | Scope / 范围 | Legacy median / P95 ms | Transaction median / P95 ms | Median delta | Conflicts / reschedules |
| --- | --- | --- | --- | --- | --- |
| 20 ms | named | 422.8 / 472.5 | 719.5 / 857.8 | +70.2% | 0 / 0 |
| 20 ms | opaque | 600.9 / 686.3 | 930.5 / 1093.1 | +54.9% | 0 / 0 |
| 20 ms | same-file | 769.3 / 900.5 | 1698.6 / 2298.1 | +120.8% | 522 / 522 |
| 100 ms | named | 615.4 / 645.6 | 853.2 / 930.7 | +38.6% | 0 / 0 |
| 100 ms | opaque | 1561.0 / 1809.9 | 879.4 / 1581.0 | -43.7% | 0 / 0 |
| 100 ms | same-file | 1520.4 / 1617.8 | 1216.4 / 1340.0 | -20.0% | 404 / 404 |
| 500 ms | named | 1797.1 / 1828.5 | 1936.0 / 2108.9 | +7.7% | 0 / 0 |
| 500 ms | opaque | 6416.2 / 6448.8 | 1866.7 / 1959.4 | -70.9% | 0 / 0 |
| 500 ms | same-file | 6432.1 / 6506.0 | 2036.5 / 2171.6 | -68.3% | 129 / 129 |

Long opaque/same-file work improves. Short operations retain material admission
and CAS overhead. These results do not establish general throughput improvement.
Integrity and fairness checks must not be reduced to hide that overhead.

长推理时的未知范围及同文件负载改善，短操作仍有明显准入与 CAS 开销。
不能宣称所有负载都更快，也不能通过删减完整性或公平性检查制造性能通过。

## OS ownership recording / OS 占用采样

A separate run uses three paired trials per scope with 100 ms inference. Intervals
are merged within each worker and then summed; totals can exceed wall time.
Lower holds span syscall success to before unlock; upper holds include the syscall
attempt and descriptor close boundaries.

另行采样每范围三组、推理 100 ms。区间在各 worker 内合并后求和，总值可能大于
墙钟。下界从加锁成功至解锁前；上界包括系统调用尝试及描述符关闭的边界。

| Scope | Legacy hold lower / upper median ms | Transaction hold lower / upper median ms | Registry / preparation median ms | Legacy / transaction inference overlap median ms | Transaction wait median / P95 ms |
| --- | --- | --- | --- | --- | --- |
| named | 1454.18 / 1454.21 | 532.03 / 532.09 | 338.91 / 320.85 | 1220.45 / 0.00 | 0.00 / 42.74 |
| opaque | 1450.79 / 1450.84 | 473.61 / 473.69 | 343.50 / 294.98 | 1224.37 / 0.00 | 971.10 / 1024.76 |
| same-file | 1462.03 / 1462.07 | 594.66 / 594.75 | 580.17 / 403.78 | 1220.57 / 0.00 | 1548.39 / 2642.99 |

Every transaction sample had zero resource/inference overlap. The harness rejects
overflow, unsettled OS holds, unclosed waits and lost writes. Raw per-worker phases
include preparation, acquisition, execution, inference and resource waiting.

每个短事务样本的资源锁与推理重叠均为零。记录溢出、未释放占用、未闭合等待及
丢失写入均使运行失败。原始阶段包括准备、申请、执行、推理与资源等待。

Trace wall includes snapshot/serialization/pipe costs and is excluded from the
throughput comparison. V7 epoch alignment has cross-worker sampling uncertainty;
all interval statistics use each worker's monotonic clock. The current collector
exposes its original epoch directly.

采样墙钟包含快照、序列化及管道成本，不用于吞吐量对照。V7 跨 worker 原点存在
采样偏差，区间统计均使用各自单调时钟；当前 collector 已直接提供创建时的原点。

Reproduce both workloads using the commands in [README](README.md). Retain raw
JSON, source/binary identities and load samples with each qualification.

按 [README](README.md) 的命令分别复现实验，每轮保留原始 JSON、源码与二进制
身份及机器负载采样。

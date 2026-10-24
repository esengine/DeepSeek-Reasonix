# Coordination qualification / 协调机制验收

Recorded results and limits: [qualification](QUALIFICATION.md).
已记录的结果及边界见[测量记录](QUALIFICATION.md)。

Use a fixed workload and alternate legacy/transaction order on the same machine.
Each worker is a real process and writes real files. The parent verifies every
expected line occurs exactly once. Model inference is simulated.

```sh
go build -o /tmp/reasonix-coordination ./benchmarks/mutation-coordination
/tmp/reasonix-coordination -trials 20 -workers 4 -steps 3 \
  -think 100ms -write 2ms -output performance.json
/tmp/reasonix-coordination -trials 3 -workers 4 -steps 3 \
  -think 100ms -write 2ms -record-locks -output locks.json
```

`-record-locks` enables a bounded recorder in each worker. Ordinary application
execution leaves it disabled. Raw traces retain lock identity, shared/exclusive
mode, syscall attempt/success and release/close boundaries, plus worker phases.

Resource hold duration is a union within each worker, with two bounds:

- Lower: syscall success to the instant before unlock.
- Upper: syscall attempt to completion of unlock and descriptor close.

These bounds expose syscall uncertainty instead of claiming a timestamp inside
the kernel. Totals sum workers and can exceed wall time. Claim-lock probes are
included; shared compatibility, preparation, registry and liveness locks are
separately identifiable in raw traces.

Acquisition includes setup, admission and queueing. `resource_wait` uses closed
coordinator wait events in transaction mode and the legacy owner's wait account.
Preparation/registry summaries use their physical lock intervals.

Run throughput comparisons without recording. Trace wall time includes snapshot,
serialization and pipe costs, so use traces only for ownership qualification.
The collector's original Unix epoch aligns workers; intervals use monotonic time.

Recording overflow, unsettled holds, unclosed wait events, lost writes or a
transaction resource hold overlapping its model inference fail qualification.
Compare median/P95 and retain source/binary identity, flags and host load.
This harness complements real Build, lifecycle and platform tests.

同机交替运行旧模式与短事务，固定 worker、写入次数及模拟推理耗时。
子进程实际写文件，父进程检查每条预期记录恰好出现一次。

吞吐量对照关闭采样；锁证明另行开启。采样墙钟包含快照、序列化和管道传输成本，
不能当作产品吞吐量。worker 时间轴使用 collector 创建时的 Unix 原点，
区间统计使用单调时钟。

`-record-locks` 仅在验收进程内开启有界采样；普通产品运行保持关闭。
原始轨迹记录锁身份、共享/独占模式、系统调用前后边界，以及准备、申请、
执行和推理阶段。持锁区间分别报告保守下界和上界，避免把 API 返回后的时间
当作完整持锁时间，也不伪造内核内部时间点。

按单个 worker 合并重叠区间后求和，总值可能超过任务墙钟耗时。
兼容 claim 的短暂探测也计入；注册、准备、共享兼容锁和活性锁可以单独归因。
申请耗时包含准备、准入和排队；等待指标沿用各协议的权威等待记录。

记录溢出、未结束的 OS 占用或等待、丢失写入，以及新版资源锁跨越推理阶段，
都会使验收失败。报告中位数、P95、源码及二进制身份、参数和机器负载。
该工具不能代替真实装配、生命周期和平台回归。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Waiting registry comparison / 等待登记性能对照

## Method / 方法

Run the reproducible driver from the repository root:

```sh
python3 scripts/benchmark-mutation-wait.py \
  --baseline 4775113397aa80f96e32f6fa78d66f99f6c5a6d7 \
  --out /tmp/mutation-wait-comparison
```

The driver builds the same current tree twice. The control replaces only
`coordinator.go` with that commit's preceding caller. Both binaries receive the
same test-only registry scan/read counters through a compiler overlay. The
on-disk product tree is never rewritten; assertions reject changed anchors.

脚本在同一当前源码上构建两个版本，仅对照组替换为上述提交的旧调用者。两者通过
编译 overlay 注入相同计数器，不修改产品文件；锚点变化时必须重新审查计数位置。
它不是“整个旧版本对整个新版本”的端到端比较。

- Holders: 1, 16, 64; waiters: 0, 1, 8, 32; three trials per variant/configuration.
- All 72 fresh child processes completed successfully; each uses real files and
  public `Acquire`, with holders/waiters inside that one process.
- Seed 14025 shuffles variants and configurations, including zero-waiter baselines.
- Counters cover 300 ms after every waiter emitted its first waiting notice.
  Active holders remain held until cancellation; no throughput is measured.
- CPU and wall time include setup, admission, observation, cancellation and cleanup.
- Both per-waiter cancellation and per-configuration P95 use nearest rank.
  With three trials the reported outer P95 is the maximum, not a stable tail estimate.

每组使用真实文件、实际事务入口，等待者都报告等待后才统计 300 ms。CPU 和总耗时
包含准备与清理。72 个子进程分别使用临时目录，全部结束；这不是跨进程争用负载，
也不测模型推理、产品任务吞吐或最终整合耗时。

## Host conditions / 主机条件

Measured on macOS 27.0.1 arm64, 10 logical CPUs and 64 GiB RAM. The shared
workstation was busy: load averages at start were 81.86/83.09/54.37, and at end
30.06/61.67/50.64. Root tests and static validation also ran during this sample.
Other tasks and VMs were neither stopped nor modified.

同机交错降低环境差异，但负载仍显著变化，且同时执行本地验收。不能把以下结果
称为空闲主机基准，也不能从少量样本推导固定延迟承诺或所有场景更快。

## Results / 结果

Cells show median / P95. CPU/wall/cancellation use milliseconds. Cancellation
columns summarize each trial's per-waiter P95. Zero waiters have no cancellation.

单元格为中位数 / P95；时间单位 ms。取消列汇总各轮等待者取消延迟的 P95，
不是把所有轮的等待者混合统计。

One holder / 一个持有者:

| Waiters | Reads before | Reads after | CPU before | CPU after | Wall before | Wall after | Cancel before | Cancel after |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 0 / 0 | 0 / 0 | 14.62 / 19.81 | 9.68 / 11.47 | 411.44 / 508.14 | 367.28 / 383.76 | — | — |
| 1 | 26 / 26 | 26 / 28 | 16.21 / 17.35 | 18.32 / 32.84 | 358.57 / 385.60 | 400.23 / 851.22 | 0.30 / 0.33 | 3.70 / 5.93 |
| 8 | 936 / 939 | 117 / 117 | 74.67 / 76.10 | 50.42 / 51.09 | 650.00 / 655.68 | 756.56 / 761.10 | 43.45 / 53.31 | 50.66 / 63.50 |
| 32 | 8669 / 9061 | 462 / 462 | 453.44 / 465.05 | 156.27 / 181.40 | 1571.97 / 1742.20 | 1019.34 / 1572.25 | 143.49 / 232.90 | 72.31 / 192.17 |

Additional registry sizes / 其他登记规模:

| Holders | Waiters | Reads before | Reads after | CPU before | CPU after | Wall before | Wall after |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 16 | 0 | 0 / 0 | 0 / 0 | 86.41 / 96.90 | 96.74 / 134.62 | 851.03 / 912.62 | 940.18 / 1815.29 |
| 16 | 1 | 221 / 221 | 221 / 221 | 166.44 / 190.75 | 176.93 / 214.19 | 2106.31 / 2740.74 | 1642.41 / 1886.41 |
| 16 | 8 | 2472 / 2496 | 312 / 312 | 184.98 / 231.79 | 202.41 / 207.56 | 825.46 / 947.25 | 2131.86 / 2137.58 |
| 16 | 32 | 10308 / 10320 | 624 / 624 | 555.26 / 637.57 | 304.90 / 392.51 | 1729.96 / 2477.15 | 1702.31 / 3548.78 |
| 64 | 0 | 0 / 0 | 0 / 0 | 772.92 / 930.36 | 769.26 / 906.56 | 3143.82 / 6273.72 | 5047.56 / 6954.83 |
| 64 | 1 | 780 / 780 | 780 / 780 | 728.77 / 859.70 | 855.11 / 958.48 | 3629.82 / 5694.64 | 3307.18 / 7245.42 |
| 64 | 8 | 7226 / 7355 | 936 / 936 | 1036.82 / 1178.29 | 1024.88 / 1101.08 | 3606.56 / 5855.55 | 5693.60 / 5770.70 |
| 64 | 32 | 10382 / 10529 | 1152 / 1248 | 1244.34 / 1244.84 | 882.29 / 1060.27 | 3569.90 / 4239.52 | 2951.42 / 4140.18 |

The repeated-read reduction is clear with multiple same-process waiters. Wall
and cancellation time are mixed, including regressions in some configurations;
one waiter has no coalescing benefit. Initial enqueue, alias filesystem checks
and compatibility/capacity discovery remain outside the optimization.

多个同进程等待者的重复读取明显减少；总耗时与取消延迟并非各组都改善，表格保留
了退化项。单等待者没有合流收益。首次入队、旧声明及容量发现成本仍存在。

The driver writes every raw trial and the complete summary, including all
configuration cancellation metrics. Correctness comes from deterministic
interleavings and fresh locked admission, not a timing threshold. See the
[security and compatibility assessment](MUTATION_WAIT_OBSERVATION_SECURITY.md).

完整取消数据在脚本生成的 raw/summary JSON 中。正确性依据是确定性交错与实际
准入复查，不能由扫描数下降或任意毫秒阈值代替。最终产品及多平台性能仍需另验。

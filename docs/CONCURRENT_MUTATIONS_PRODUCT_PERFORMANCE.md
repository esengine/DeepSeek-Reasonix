---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Product concurrency measurements / 产品并发测量

## Method / 方法

Four processes each build a Controller, bind a session lease and perform three
mutations. Only the provider is simulated. Actual tools, fixture Go tests,
persistence, shutdown and process exit are measured.

四个进程分别装配 Controller、绑定会话租约并变更三次。只有模型响应是模拟的；
实际运行工具、fixture Go 测试、持久化、关闭和进程退出。

Each workload has 20 paired trials with alternating mode order and fresh homes
and workspaces. Wall starts after all workers are ready. The parent independently
checks all final bytes. Preparation is reported separately.

每种负载有20组配对试验，交替模式顺序，使用独立home和工作区。
墙钟从全部worker就绪开始，准备单独记录。父进程独立核对全部最终字节。

Both policies first write the same Bash admission marker, letting legacy acquire
workspace scope before narrow claims. Separate unprimed runs reproduced legacy
scope-widening conflicts; this table compares completing workloads.

两种模式先执行相同的Bash准入标记写入，让旧模式先取得工作区范围，再申请窄路径。
未执行此前置操作的旧模式另有范围扩张冲突复现，不能被本对照掩盖。

Host: macOS ARM64, Go 1.26.5, ten logical CPUs. The 500 ms/trace run sampled
one-minute load 34.109–182.376; other local Go checks overlapped.
These are paired same-host measurements under heavy load, not idle-host claims.

宿主为macOS ARM64、Go1.26.5、10个逻辑CPU。500ms/轨迹组的一分钟负载为
34.109–182.376，期间有其他本地Go检查。这是高负载同机配对结果，不代表空闲性能。

## Wall time / 墙钟

Milliseconds, median / nearest-rank P95. Conflicts/reschedules total all 20 shared
trials. Each version conflict triggers a fresh read; unrelated errors abort.

单位ms，每格为中位数/最近秩P95。冲突/重调度为全部20次共享试验的总数。
每次版本冲突都重新读取；其他错误直接终止。

| Provider wait / 请求等待 | Scope / 范围 | Legacy | Shared / 共享事务 | Conflicts / reschedules |
| --- | --- | --- | --- | --- |
|100ms|named / 不同文件|6816.83 /10493.73|5210.41 /9130.10|0 /0|
|100ms|opaque / Bash|6561.49 /10169.71|5230.60 /9388.56|0 /0|
|100ms|same-file / 同文件|8022.48 /11114.90|6109.95 /8815.43|166 /166|
|500ms|named / 不同文件|15090.42 /15140.99|6899.42 /7037.42|0 /0|
|500ms|opaque / Bash|14918.27 /17473.00|6682.40 /6771.58|0 /0|
|500ms|same-file / 同文件|21316.23 /25187.76|11278.70 /14668.47|92 /92|

Wait is per provider request, not per task. Reads and conflict retries add
requests, so shared same-file inference totals can exceed legacy. All expected
markers were retained without duplicates or lost writes.

等待按模型请求计算；读取和冲突重调度会增加请求，因此共享同文件组累计推理可能
高于旧模式。所有预期标记都保留，无重复或丢失。

## Physical lock intervals / 物理持锁区间

Three further pairs per scope use 100 ms provider waits and OS lock recording.
Values below are medians in milliseconds, summed over workers. Concurrent spans
can exceed wall time; columns are not additive partitions.

每个范围另运行3组，请求等待100ms，采集OS锁边界。下表为跨worker累加的ms中位数；
并发区间可能超过墙钟，各列不能直接相加。

| Scope / mode | Resource hold lower–upper / 资源持锁 | Tool dispatch / 工具区间 | Registry / 注册 | Preparation / 准备 | Hold during inference / 推理时持锁 |
| --- | --- | --- | --- | --- | --- |
|named /legacy|6837.666–6837.689|15505.272|81.960|0|2029.764|
|named /shared|6114.436–6114.623|16571.804|1310.414|661.692|0|
|opaque /legacy|6645.126–6645.162|14724.122|111.113|0|2035.000|
|opaque /shared|4866.463–4866.647|16198.409|1060.180|722.784|0|
|same-file /legacy|8122.748–8122.785|17094.198|111.849|0|3245.554|
|same-file /shared|5267.328–5267.530|12235.462|1456.256|814.977|0|

Resources include actual fixture verification commands. Lower bounds exclude
lock syscalls; upper bounds include them. Unclosed holds or shared resource
holds overlapping inference fail analysis; all traced rounds passed.

资源区间包含实际fixture验证。下界不含加解锁系统调用窗口，上界包含。
未闭合持锁或共享资源持锁与推理重叠都会使分析失败；本轮全部轨迹通过。

Tool dispatch includes admission and waiting, not just execution. Raw summaries
also retain notified transaction waits. Equivalent legacy events are unavailable
and omitted rather than reported as zero. No-recording runs omit OS intervals.

工具区间包含准入和等待，不等于纯执行。原始摘要另记录事务等待通知；旧模式缺少
同等事件，省略该指标而非显示零等待。未采样的运行不提供OS持锁数值。

## Provenance and limits / 身份及边界

| Artifact / 产物 | SHA-256 |
| --- | --- |
|v3 source archive (100ms)|a1eab5a1c1feedd07a078efc08155f80ed48b998567960858a7528116dfbd211|
|v3 binary|633b8c19a1ed0ff8ab74898d742a018a433da01b3743f1733d85c26090cb5f19|
|v7 source archive (500ms/traces)|e0810ccaf5d5afc5989385fdb84e08bb392ed2cd393987b61b9be8394cc7f989|
|v7 binary|100e3d324e2351d00ad291325c50a396fc9650f15d4848245409980d1b95bacc|
|schema 2 analyzer|640491db69dda17f586532903945eecef616b2426a4bc1ec3de2d7afd34eac22|

The qualification bundle retains raw reports, worker traces, host samples and
source/binary identity. Commands and negative probes are in
[the benchmark guide](../benchmarks/product-concurrency/README.md).

验收包保留原始报告、worker轨迹、负载采样及源码/二进制身份。
复现命令和失败/取消探测见[基准说明](../benchmarks/product-concurrency/README.md)。

An earlier 500 ms run stopped during opaque/legacy preparation with EOF. The old
harness discarded worker stderr, so the cause remains unconfirmed. That failed
run is retained and contributes no successful trial to these tables.

此前500ms运行在opaque/legacy准备时遇到EOF。旧基准未保留worker stderr，原因
仍未确认。失败记录保留，不计入表中的成功试验。

The harness now atomically saves each round and failed-worker diagnostics.
Partial reports are rejected. Real worker-failure and cancellation probes pass.
The replacement run establishes these measurements, not the earlier EOF's cause.

基准现逐轮原子保存并保留失败诊断，分析器拒绝部分报告。真实worker失败和取消探测
通过。替代运行提供本报告的测量数据，不能证明此前EOF根因。

Isolation, fleet, recovery, frontend interaction and controlled verification
have separate correctness evidence; this fixture measures independent shared sessions.

本基准测量独立会话共享协作；隔离、fleet、恢复、前端与受控验证使用各自正确性证据。

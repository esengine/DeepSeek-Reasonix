---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Waiting observations / 等待观察复用

## Cause and owner / 根因与所有者

Each path-blocked request formerly scanned every registry record on each
20 ms retry. With W waiters and H holders, the repeated file reads grow roughly
as W × (H + W). The mutation package owns both this polling and the final grant;
coalescing belongs here, rather than in task scheduling or tool definitions.

原先每个具名路径等待者每 20 ms 重扫全部登记文件，产生约 W × (H + W) 的重复
读取。本批只在事务所有者内合并等待观察，不修改调度、工具定义或默认工作模式。

## Source, sink and authority / 来源、操作与授权

- Input is the existing registry namespace and validated `record` values read
  under its existing OS guard. Parsing, unsupported-version rejection and dead
  record recovery remain owned by `records`; the pool introduces no file writer.
- A process-local pool shares an immutable completed observation for at most
  the existing retry interval, measured from completion. It has at most 128
  namespace entries. That bounds entry count, not total registry bytes.
- Only a retry following a real path-conflict transaction blocker uses the pool.
  Initial admission, nonqueuing admission, legacy-only and capacity-only
  blockers keep the existing fresh path.
- A cached blocker may only defer a retry. An apparently free observation
  always enters `tryGrant`, including fresh registry/queue/capacity checks,
  compatibility claim acquisition and the final locked publication recheck.
  Cached data never authorizes a grant or expands a resource set.
- The key is the existing registry directory spelling. Aliases can duplicate
  observations, reducing the benefit; they do not replace grant resource identity.

来源仍是 OS 注册锁内读取、验证的现有记录。缓存只能让调用继续等待；“未发现阻塞”
必须重新走原准入路径、旧声明及发布前复查。缓存不是权限或租约。路径别名只可能
降低复用率，不建立另一个授权域。128 是条目数量上限，不是读取字节上限。

Path conflicts include normalized named paths and expanded workspace/Git
metadata resources. The `NamedPaths` blocker category describes the conflict,
not necessarily the request's effect. Alias identity checks still inspect the
filesystem; only registry observations are shared.

路径阻塞也包括展开后的工作区及 Git 元数据资源；阻塞类别不等于原请求的影响类别。
文件别名身份检查仍访问文件系统，本批只合并登记观察。

## Lifetime and failure / 生命周期与失败

- The first caller loads synchronously; the pool owns no goroutine or timer.
- Followers wait on completion or their own context; their cancellation cannot
  cancel the reader.
- A canceled reader is forgotten with pointer identity checking. Live peers
  retry instead of inheriting that cancellation.
- A reader panic closes completion with `ErrCallback`. Context is checked again
  before consuming a completed result.
- Only completed entries are evicted. If all 128 are reading, an extra namespace
  performs its own real read without enlarging the table.

首个调用同步读取，不新增后台 goroutine。跟随者独立取消；读取者取消后其他调用
重新读取，不能继承其取消结果。panic 转为明确错误并关闭完成通道。清除必须匹配
原观察身份，不能删除替换后的条目。全部条目正在读取时回退实际读取，不扩大缓存。

Observations can delay seeing release, cancellation, death or malformed records
until the next refresh. A new admission still reads immediately. Malformed or
unknown records refuse admission and preserve their bytes. A stale observation
never releases another lease.

The final registry remains the FIFO authority;
disjoint resources retain their existing admission path. Filesystem IO already
in progress is not made preemptible by this change.

释放、进程死亡或坏记录可能延迟一个观察周期才被等待者看到；新申请仍立即检查。
坏记录拒绝准入且保留原文件，旧观察不能释放其他租约。FIFO 仍以最终登记为准。
本批不承诺中断已经进入文件系统的同步 IO。

## Compatibility and limits / 兼容与边界

| Contract / 契约 | Before and after / 变更前后 |
| --- | --- |
| Registry record/version and stable mutex inode | Unchanged; previous readers and writers see the same files. |
| Public API, grants, outcomes and notices | Unchanged; no serialization, prompt or tool-definition change. |
| Capacity-only and legacy-only contention | Existing behavior; no claim of eliminating their scan cost. |
| Cross-process writers | Existing OS locks remain authoritative; observations are not shared between processes. |
| Runtime/default activation | None; this remains a library-level prerequisite. |

This is not a sandbox, unknown-scope hardlink repair, complete queue performance
solution, short-transaction runtime activation or recovery journal. Legacy
cross-workspace/optimistic bypass limitations remain documented in
[the primitive assessment](MUTATION_COORDINATOR_SECURITY.md).

本批不是沙箱、未知范围硬链接修复、完整性能验收或运行时接入；旧协议跨工作区及
乐观绕过边界保持原说明。作者的分析不替代独立安全审查。

## Verification / 验证

Deterministic tests force coalescing, expiry, follower/reader cancellation,
panic closure, bounded eviction and replacement identity. Real registry tests
retain a competing transaction and new legacy claim behind an empty observation,
and introduce an unsupported record while waiting.

Existing owner tests cover
queue order, disjoint paths, process death, capacities and completion cleanup.

确定性交错覆盖合流、过期、取消、panic、条目上限及替换身份；真实登记测试保证
空观察不能越过新增事务或旧声明，未知版本被拒绝且保留文件。性能比较使用真实
`Acquire`、独立子进程及同机交错样本，单独报告扫描次数、CPU、耗时和取消延迟；
不把单进程协作等待的结果称为跨进程争用或最终产品验收。

Reproduce the isolated caller comparison on macOS/Linux with Python 3 and Go:

```sh
python3 scripts/benchmark-mutation-wait.py \
  --baseline 4775113397aa80f96e32f6fa78d66f99f6c5a6d7 \
  --out /tmp/mutation-wait-comparison
```

该脚本通过编译 overlay 替换旧调用者并注入相同计数器，不修改产品源码。输出目录
必须尚不存在。每组默认三个独立子进程样本；最近秩 P95 等于最大值，不能当作稳定
尾延迟估计。摘要记录主机负载；CPU 和总耗时包括准备、等待及清理。

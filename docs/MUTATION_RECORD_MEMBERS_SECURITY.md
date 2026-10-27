---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Mutation record members / 写入事务记录字段

## Scope and cause / 范围与根因

The coordination reader must distinguish a supported record from bytes whose
meaning it cannot establish. Go's struct decoder accepts repeated JSON members
and matches field names without regard to case. Unknown-field rejection alone
does not close either ambiguity.

A constructed record containing `"version":99,"version":1` is read as version
1. An unrelated public acquisition can then proceed; once the record's OS
owner is dead, the same scan deletes it. Repeated resource members and aliases
such as `Version` or capacity `Units` have the same decoding boundary.

Normal shipped writers use `json.Marshal` and do not generate these shapes.
Tests deliberately alter host-selected coordination records. This is protocol
hardening within trusted private state, not evidence of a production incident
or remote access to the registry.

统一读取器必须确认记录含义。Go 结构体解码会合并重复字段、接受字段名大小写别名，
仅拒绝未知字段不足以识别歧义。人为重复版本或资源字段可使记录被当作已知协议继续
准入，并在持有者死亡后被清理。

正常写入器不会生成这些形状；测试主动修改可信状态目录中的记录，不代表线上事故
或远程攻击者能够写入注册表。

## Source, authority and sink / 输入、权威与副作用

- Input remains `mutations-v1/*.json` in the host-selected state directory.
  No tool argument gains authority over that directory.
- Before typed decoding, the common reader requires one JSON object with
  unique decoded member names and exact declared field spelling. Each capacity
  object follows the same rule.
- A unique escaped spelling such as `\u0076ersion` still denotes `version`.
  Repeating it alongside `version` is refused. Whitespace and field order
  remain unrestricted; this is not a canonical-byte JSON format.
- Existing version, identity, effect, absolute-path and capacity validation
  still runs before liveness checks and dead-owner scavenging.
- Refusal is `ErrProtocol` with the existing quoted record filename. The
  offending bytes are preserved, the request is not published, and another
  owner's OS lock is not released.

字段校验发生在类型解码、存活探测和回收之前。顶层和容量对象均要求字段唯一、名称
精确匹配；转义后的名称用于重复判断，但不限制合法空白和字段顺序。

错误保留 `ErrProtocol` 身份与记录文件名。拒绝后不改动该记录，不发布新申请，也不
释放其他持有者。既有 OS 锁和资源校验仍是授权边界。

## Compatibility / 兼容性

| Input | This reader | Preceding reader |
| --- | --- | --- |
| Shipped v1/v2 encoding, including omitted v1 effect | Recognized | Recognized |
| Unique exact names, reordered or spaced JSON | Recognized | Recognized |
| Unique escaped exact name | Recognized | Recognized |
| Duplicate top-level or capacity member | Refuse; preserve | May merge and scavenge |
| Differently cased declared name | Refuse; preserve | May accept and scavenge |
| Unknown field/version or trailing JSON value | Refuse; preserve | Already refuses |

Even identical repeated values are refused: interpreting a duplicated authority
declaration is not a supported compatibility policy. An old program still
accepts ambiguous records; this reader cannot fence a non-cooperating older
writer or reader.

No writer encoding, protocol version, default or runtime
activation changes.

正常 v1/v2 写入、省略旧影响字段、字段换序、空白和唯一转义名称保持兼容。即使重复
值相同也拒绝；旧程序仍可能接受歧义记录，本次不能单方面约束旧进程。不改变写入
格式、协议版本或默认运行策略。

## Evidence and limits / 验证与边界

Public path and capacity admission tests cover live and dead owners, repeated
version/effect/path/active/capacity fields and case aliases. A real subprocess
publishes a normal record; tests alter it, check refusal, kill and join that
child, then check preservation again. Known records retain normal reclamation.

The preceding production decoder compiles with these behavior tests. It admits
requests or removes ambiguous records, while unambiguous JSON controls pass.
Native and hosted evidence belongs to the exact PR head; compilation does not
establish native execution.

公共申请覆盖具名路径和额度、存活与死亡持有者，以及重复版本、影响、路径、活跃状态
和容量字段。真实子进程测试包含发布、修改、拒绝、终止并等待该进程、再次拒绝及字节
保留；正常记录仍可回收。旧解码器能编译，但在行为回归中确实错误准入或删除。

This does not authenticate files, bound registry reads, harden the filesystem
namespace, solve tree-external hardlinks, or activate shared transactions.

Parsing performs additional work per scan; prior waiting-observation benchmarks
are not performance results for this decoder.

A scan can reclaim understood
records before encountering an ambiguous one; it is not an atomic registry
snapshot. This author's assessment is not independent security approval.

本次不完成认证、读取大小限制、目录句柄加固、树外硬链接或共享事务激活。解析增加
每次扫描的工作量，不能沿用等待观察优化的性能数字。扫描可能先回收其他已知记录，
并非整个目录的原子快照。本文是作者评估，不代替独立安全审查。

---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Controlled container verification / 受控容器验证

Host-selected checks can use an immutable Linux container through the same
verification owner used by isolated tasks and integration candidates. Local
Docker Unix sockets and Windows named pipes are supported.

隔离任务和整合候选共用的验证入口可以在不可变 Linux 容器中运行宿主检查。
支持本地 Docker Unix socket 和 Windows 命名管道。

## Provisioning / 准备环境

Declare a trusted, already-local image by its complete immutable ID in the
**user** configuration. Project configuration cannot replace this selection.

在**用户配置**中声明可信、已位于本地的镜像，使用完整不可变 ID。
项目配置不能替换此选择。

```toml
[agent]
verification_runtime = "docker-image://sha256:<64 lowercase hexadecimal characters>"
```

Get the ID with `docker image inspect --format '{{.Id}}' YOUR_LOCAL_RUNTIME`.
Tags, implicit pulls and TCP/SSH endpoints are rejected. Simultaneous
`DOCKER_HOST` and `DOCKER_CONTEXT` selections are rejected as ambiguous.

可用 `docker image inspect --format '{{.Id}}' YOUR_LOCAL_RUNTIME` 获取 ID。
拒绝 tag、隐式拉取、TCP/SSH 端点，以及同时指定 `DOCKER_HOST` 和
`DOCKER_CONTEXT` 的歧义配置。

The host resolves its endpoint or current Docker context, then pins that local
endpoint. CLI, ACP, scheduled execution, serve and Desktop's serve host supply
the launch inputs. Isolated and nested runtimes inherit independent copies.

宿主解析端点或当前 Docker context，随后固定本地端点。
CLI、ACP、计划执行、serve 和 Desktop 的 serve 宿主传入启动信息；
隔离及嵌套运行时独立复制这些信息。

The image and Engine must both be Linux with the same architecture (`amd64` or
`arm64`). The image is part of the trusted verifier. Image-declared volumes are
unsupported.

镜像和 Engine 必须同为 Linux，且架构一致（`amd64` 或 `arm64`）。
镜像属于可信验证器。不支持镜像声明的 volume。

Provision Bash 5.1 or newer, `/usr/bin/setpriv`, `/usr/bin/env`, `/usr/bin/cat`,
and the offline toolchains/dependencies required by the checks.

需要 Bash 5.1 以上版本、`/usr/bin/setpriv`、`/usr/bin/env`、`/usr/bin/cat`，
以及检查所需的离线工具链和依赖。

No credentials, writable host dependency directory or Engine socket is mounted.
Image environment variables are removed before the root supervisor starts.
Declared check variables reach only the unprivileged payload.

不挂载凭证、宿主可写依赖目录或 Engine socket。
启动根监督进程前清除镜像环境变量。检查声明的变量仅传给降权后的进程。

## Execution / 执行

The host freezes supported source inputs and substitutes captured host-held
criteria only in that copy. It uploads the copy under a fresh source root.
Absolute or escaping source links are rejected.

宿主冻结受支持的源码输入，仅在副本中替换宿主保存的测试准则，
并上传到新的源码根目录。拒绝绝对链接或越界链接。

Before execution, every received entry's content, type, mode, owner and link
target must match the frozen copy. Original source files remain unchanged.

执行前逐项核对收到文件的内容、类型、模式、所有者及链接目标。
不修改原始源码。

The payload runs as UID/GID 65534 without capabilities or new privileges. Source
and root filesystems are read-only. PID, IPC and cgroup namespaces are private;
there is no external network interface.

执行进程使用 UID/GID 65534，无 capabilities 且不能获取新权限。
源码和根文件系统只读，PID/IPC/cgroup 命名空间独立，没有外部网络接口。

Private temporary storage permits executable build output. The payload cannot
access the root-owned completion receipt or supervisor stdin.

私有临时文件系统允许执行编译产物。执行进程无法访问根用户独占的
完成记录或监督进程 stdin。

The supervisor waits for the host to read completion and close the liveness
pipe. Its exit tears down the PID namespace and remaining descendants.
Host death closes the pipe too. Cancellation checks the exact container ID and owner.

宿主读取完成记录并关闭活性管道后，监督进程退出，PID 命名空间内后代一同结束。
宿主死亡也会关闭管道。取消操作核对具体容器 ID 和所有者。

## Evidence / 证据

Receipts bind actual Docker and Bash bytes, command, declared environment,
Engine identity/version/kernel/architecture, immutable result image, container
policy and source snapshots.

凭据绑定 Docker 和 Bash 的实际字节、命令、声明环境、Engine
身份/版本/内核/架构、不可变结果镜像、容器策略及源码快照。

A receipt is minted after the process scope stops and inputs still match.
A failed check can have a valid environment receipt; the check still failed.
Captured-criterion receipts cannot authorize ordinary integration.

仅在进程范围完全结束、输入仍一致后生成凭据。失败的检查可以具有有效环境
凭据，但仍是检查失败。测试准则凭据不能授权普通整合。

Restarted processes cannot trust serialized receipt claims. Ordinary commands
and unavailable backends never receive valid controlled evidence.

重启后不能信任序列化数据自称有效的凭据。
普通命令和不可用后端不会获得有效受控证据。

## Recovery and limits / 恢复与边界

Creation starts with a versioned local journal and OS owner lock. Recovery skips
live owners, checks the Engine/endpoint and ownership labels, then removes only
that invocation's containers and temporary image.

创建前持久化带版本的日志并持有 OS 所有者锁。恢复跳过活跃所有者，
核对 Engine/端点及标签，仅清理本次调用的容器和临时镜像。

Unknown journal fields or versions are retained and reported. Daemon loss, incomplete
observation, input changes or unproven teardown keep verification unknown.
Candidates remain available for retry or explicit management.

未知日志字段或版本保留并报告。daemon 不可用、观察不完整、输入变化或无法证明
进程已结束时，验证保持未知。候选保留供重试或明确管理。

Docker, its daemon and the declared runtime remain trusted authorities.
This bounded verifier does not isolate project databases, remote MCP services,
other task tools' credentials or all shared Git metadata.

Docker、daemon 和声明的运行时仍是可信权限主体。
此验证器不隔离项目数据库、远端 MCP、其他任务工具的凭证或所有共享 Git 元数据。

External processes bypassing coordination retain the documented detection and
recovery boundary. The native Linux offline-root backend remains available.

绕过协调协议的外部进程仍遵守已说明的检测及恢复边界。
Linux 原有的离线根文件系统后端继续可用。

See [the mutation protocol](CONCURRENT_MUTATIONS.md) and
[fleet behavior](CONCURRENT_MUTATIONS_FLEET.md) for authorization and integration.

授权及整合参见[变更协议](CONCURRENT_MUTATIONS.md)和
[fleet 行为](CONCURRENT_MUTATIONS_FLEET.md)。

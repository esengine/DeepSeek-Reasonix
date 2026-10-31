---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-28
---

# Model connection checks / 模型连接检测

Automatic checks are **off by default**. Enable **Automatically check connections
and balances** in model connection settings to opt in. Opening Studio's chat
model picker then checks authorized user-global connections in the background.

Rows show connection/authentication failures, payment or quota refusals, temporary
rate limits, and insufficient balance reported by a configured wallet endpoint.

Checks keep every model and the current selection; use **Manage models and
connections** to repair a connection or run an explicit model trial.

The picker keeps its existing account groups, visible endpoint hosts, row density
and current-selection behavior. Opting in adds one status chip to each model row;
hovering the row or reading its accessible description reveals the full diagnostic.

The switch applies to every authorized global connection and sits outside the
individual service editor. Older servers show an upgrade hint with a disabled
switch; a failed settings read shows a connection/permission hint and runs no probes.

The automatic check reads the model listing and configured wallet only. It sends
no completion requests. A reachable listing does not verify that a particular
model can generate; an unsupported, unreadable, or unconfigured wallet stays
unknown.

Resolver-backed and extension catalogs are not checked against local
credentials. Servers without provider-edit permission do not run these probes.

Project configuration cannot opt in or change probe destinations or proxies;
project-only connections must first be saved globally. All redirects are refused,
including same-origin redirects.

Cached entries contain only a credential/config
digest, status classes and timestamps, never raw keys, responses or balances.

Checks run up to three connections at a time in the picker and share an in-flight
request within one runtime. Results are cached for one minute; reopening the
picker refreshes expired results. Changing credentials, connection settings, or
proxy configuration invalidates the cache. Results are kept in memory only.

自动检测**默认关闭**。在模型连接设置中开启「自动检测连接与余额」后，打开模型
列表才会在后台检查已授权的全局连接。模型行会提示
连接或鉴权失败、付费要求、配额耗尽、临时限流，以及余额接口报告的余额不足。
检测保留所有模型与当前选择；可通过「管理模型与连接」修复配置或主动试调用模型。

模型列表保留原账户分组、可见接口域名、行密度和当前选择行为。开启检测后仅在
模型行增加一个状态标签，悬停或读屏说明提供完整诊断。

开关作用于全部已授权全局连接，位于单个服务编辑器之外。旧服务器显示升级提示
并禁用开关；设置读取失败时提示检查连接或权限，且不执行探测。

自动检测只读取模型列表和已配置的余额接口，不发送生成请求。列表连通不等于
每个模型都能调用；不支持、未配置或无法读取的余额保持未知。远程解析器和扩展
模型不会拿本机凭据进行检测；没有服务商编辑权限的服务端也不会执行这些探测。

项目配置无法开启检测或更改检测地址及代理；项目专属连接需先保存到全局。
所有跳转（含同源跳转）均被拒绝；缓存仅保留配置及凭据摘要、状态和时间戳，
不保留原始密钥、响应内容或余额数值。

菜单最多同时检查三个连接，同一运行时复用正在进行的检测。结果缓存一分钟，
过期后重新打开菜单会再次检查；修改密钥、连接或代理配置会使旧缓存失效。
检测结果只保留在内存中，不写入配置，也不自动切换模型。

## Compatibility / 兼容性

| Contract / 合约 | Older client / 旧客户端 | New client / 新客户端 |
| --- | --- | --- |
| Health settings API / 检测设置 API | Missing route / 无路由 | Missing/error means no automatic probes / 缺失或错误时不发自动检测 |
| `desktop.provider_health_checks` | Absent means off / 缺省关闭 | User-global opt-in only / 仅用户全局配置可开启 |

Requirements and scope decisions / 需求与范围记录:
[Discussion #14617](https://github.com/esengine/DeepSeek-Reasonix/issues/14617).
The grouped layout is retained per maintainer feedback / 按维护者反馈保留分组布局:
[Review](https://github.com/esengine/DeepSeek-Reasonix/pull/14634#issuecomment-6449927590).

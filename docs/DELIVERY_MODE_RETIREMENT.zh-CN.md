> 后续变更：执行与验收机制已进一步精简。当前行为见 [执行模型迁移说明](EXECUTION_MODEL_SIMPLIFICATION.md)；本文保留交付模式退役的接口兼容背景。

# 交付模式退役

[English](DELIVERY_MODE_RETIREMENT.md)

Reasonix 不再提供“交付”会话模式。新建和恢复的本地会话统一采用标准执行。
Plan、Goal、项目检查、任务风险规则、用户显式验证要求和真实测试记录继续独立生效。

旧 `/preset`、`/work-mode`、`/profile`、HTTP、桌面和 ACP 输入作为兼容入口保留。
已知旧值仍会校验并返回退役说明，但不会改变运行策略；未知值仍报错。新状态与正常
持久化写入固定使用 `qualityFloor=standard`、`agentPreset=standard` 和 `tokenMode=full`。

历史检查点和回执原样保留。重建当前任务合同时，历史 `policy_floor=delivery` 印记不再
产生要求；普通任务过去的暂停只作为历史信息展示，不再成为当前阻断。Plan 和 Goal
检查点继续使用原有恢复规则。Reasonix 不会清空缺项，也不会把失败证据改写成通过。

桌面客户端不会向远端发送策略切换。旧远端若返回 Delivery 状态或由它产生的暂停，
客户端会保留真实状态和恢复操作，并按远端会话显示一次升级提示；升级远端服务后才会
移除旧策略。新格式会话降级后仍可读取，但旧程序可能根据历史字段再次执行 Delivery
行为；数据兼容不代表旧运行时遵守新语义。

# 模型设置运行时验证

本记录区分已实现行为与发布资格。最终候选必须通过完整矩阵，包括 Windows 原生升级验收，才能发布。

| 需求 | 实现归属 | 证据 |
| --- | --- | --- |
| 无活动会话保存；默认模型只影响新会话 | `desktop/model_settings_api.go`、`desktop/model_settings_preferences.go` | `TestModelSettingsSaveWithoutActiveSession`、默认模型与新会话测试 |
| 校验和并发比较先于保存；保留未知字段 | `internal/config/model_settings_edit.go`、统一 API 操作字段白名单 | `TestModelSettingsRequestRejectsStaleEdit`、`TestSaveModelSettingsPreservesUnknownFields`、目录提交并发测试 |
| 独立凭据引用、分组连接与失败恢复 | `internal/config/model_credential_commit.go` | `TestModelSettingsGroupedCredentialCommitAndReceipt`、`TestModelSettingsFailedCommitCleansOnlyItsStagedCredential`、`TestModelSettingsCredentialWriteFailureKeepsConfig` |
| 当前工具续轮保持原连接，下一轮刷新 | `internal/config/model_runtime_snapshot.go`、`internal/boot/boot.go`、`desktop/turn_admission.go` | `TestModelSettingsRunningToolContinuationKeepsOldConnection`、凭据冻结测试 |
| 队列后续消息与机器人请求经过新运行边界 | `internal/control/inbox_dispatch.go`、`internal/bot/model_settings.go` | `TestModelSettingsQueuedFollowupAppliesLatestBeforeDispatch`、`TestBotNewRunAppliesModelSettingsAndKeepsSessionOnFailure` |
| 项目覆盖及非活动、后台分离会话分别应用 | 运行时所有者查找和既有 Desktop 重建流程 | `TestModelSettingsProjectOverrideSkipsRebuild`、`TestModelSettingsRemovalRetryKeepsFailedTargetAndAppliesInactiveSibling`、`TestModelSettingsRetryAppliesDetachedRuntimeWithoutCreatingTab` |
| 启动和连续保存的发布保护 | 启动版本校验、携带版本的延迟重建项 | `TestModelSettingsStartupPublicationRejectsCandidateBuiltBeforeSave`、延迟重建测试 |
| 最终 lease 失效保留旧运行时并允许安全重试 | 最终 authority 绑定、拒绝失效写入和迁移快照前的 authority 恢复 | `TestModelSettingsFinalAuthorityFailurePreservesRuntimeAndRecovers` 在最终绑定前释放真实 lease |
| 删除后没有可用模型时阻止新运行，保留历史 | 经过校验的运行时模型选择 | `TestModelSettingsLastProviderRemovalBlocksNewRun`、服务删除测试 |
| 远程不可变路由、候选所有权和自动下一轮应用 | `desktop/cred_proxy.go`、`desktop/remote_model_settings.go`、`internal/serve/model_settings_source.go` | `TestRemoteModelOwnershipRetiresOldRouteAfterInFlightRequest`、`TestRemoteModelSourceRefreshesAutonomousHTTPRunAndRetiresOldRoute`、Serve 应用失败与回执测试 |
| 远端项目配置继续优先 | `internal/config/model_runtime_settings.go` | `TestManagedModelSnapshotPreservesProjectProviderAndAssignments` |
| 架构状态不进入模型前缀 | 仅传输层使用快照元数据，保留序列化行为 | `TestRemoteModelSnapshotPreservesWirePrefixAndKeepsKeysLocal` 比较 OpenAI、Anthropic、Responses 请求字节 |
| 已保存、待应用、失败状态及草稿竞态 | 结构化桥接结果、请求回执和读写代次 | `model-settings-receipt.test.ts`、`provider-editor-save-races.test.tsx`、设置回读快照测试 |
| 保存后创建的子代理及审批续轮保持已接受的快照 | Boot 子代理工厂和控制器审批恢复 | `TestModelSettingsChildCreatedAfterSaveInheritsAcceptedRunSnapshot`、`TestModelSettingsApprovalResumeKeepsAcceptedCredential` 验证真实 HTTP 请求 |
| 连续保存超过构建进度及完成响应丢失 | 共享运行时所有者刷新、源版本校验和候选保留 | `TestModelSettingsSourceFencesOvertakenBuildAndUncertainFinish` |
| 远程后台分离工作保留自己的接纳边界 | `internal/serve/model_settings_detached.go` | `TestDetachedModelSettingsRefreshTargetsItsOwnerAndPreservesFailure`、`TestDetachedModelSettingsKeepsQueuedOwnerUntilAdmission` |
| 远程所有权达到容量上限时不清理已接受路由 | 代理范围内的候选接纳 | `TestRemoteModelOfferCapacityPreservesOwnedRoutes` |
| 迟到回执和旧连接不能撤销当前路由 | Serve 实例及递增序列、原子连接身份绑定和路由回收 | `TestRemoteOwnershipRejectsOvertakenReceipts`、`TestRemoteReplacedConnectionCannotPinOwnership`、`TestRemoteIncarnationReclaimsOldReservationsAndRejectsLateBuilders` |
| 明确拒绝释放候选；结果未知时保留至确认 | 拒绝类型与正向所有权回读 | `TestRemoteInstallDistinguishesRejectionFromLostAcknowledgement`、`TestRemoteUnknownInstallRetainsOfferUntilOwned` |
| 控制器关闭后不再创建 inbox 文件 | inbox 打开封锁和同步登记的发布通知 | `TestClosedControllerCannotOpenInboxFromLateDispatch`、`TestStaleRecoveryCannotOverwritePublishedForegroundRoute` |
| 旧快照不能覆盖新选择的连接 | 列表投影提交期间保持当前租约代次，直到元数据写入完成 | `TestListingProjectionCannotOverwriteModelAfterAuthorityReplacement`、`TestListingAuthorityGuardRetainsGenerationThroughCommit`、`TestOwnedListingRejectsMissingAuthority`、`TestModelSettingsCredentialRefreshPersistsSelectedConnection` |
| 恢复会话后无需打开选择器即可显示正确连接 | 目录随就绪状态、会话身份刷新，并拒绝过期响应 | `model-switcher-refresh.test.tsx`，同模型双连接的原生冷启动恢复 |
| 保存后的 HTTP 重试保持已接受的凭据 | 传输重试复用不可变服务凭据 | `TestModelSettingsHTTPRetryKeepsAcceptedCredential` 返回真实 503，验证重试和下一运行 |

## 确定性验证

先执行针对性测试，再运行共享所有权的 race 测试、两个 Go 模块、前端类型检查／测试／构建及仓库检查。根模块测试不包含独立的 Desktop 模块。

```sh
go test -p 4 ./...
go test -race ./internal/config ./internal/boot ./internal/control ./internal/bot ./internal/serve
(cd desktop && go test -p 2 ./...)
(cd desktop && go test -race . -run 'TestModelSettings|TestRemoteModel|TestCredentialProxy|TestDeferred')
(cd desktop/frontend && pnpm typecheck && pnpm test:all && pnpm build)
go run ./tools/repolint
```

前端测试包含回执恢复、延迟保存草稿、远程行为和长历史性能。结果不明的写入先回读，不自动重做。候选保留机制保护构建过程，Serve 全局有序的所有权回执阻止旧状态撤销已发布路由。候选释放与路由回收原子完成；已接受的旧请求在完成时释放自己的引用。

## Windows 原生发布门槛

产品提交 `e0572c8c916c5d012770dcb2aeb98eab5361924a` 已完成 Windows 11 ARM64 原生验收，使用 Go 1.26.6、Wails 2.13.0、CGO 和生产前端资源。候选可执行文件 SHA-256 为 `C6E955C1491EC700E2A8A8806005998854A9071EFA7D552AF82F37BCA1F8B666`，官方 1.38.2 前序可执行文件为 `C20863C47A52B2D69F72E201D5FBAA3C57BC6132FF0AF69046FE0667D8E60B1D`。隔离的版本化安装沿用原启动器、配置目录和历史。请求使用本地 HTTP 夹具及一次性测试凭据，不代表真实供应商兼容性测试。

| 原生场景 | 必须达到的结果 | 验收状态 |
| --- | --- | --- |
| 隔离的 1.38.2 原地升级到候选版本 | 沿用同一配置目录和会话，不要求重新输入凭据或删除配置 | 已通过原启动器恢复保存的连接及此前本地、远程回复 |
| 无活动会话下的模型偏好、模型服务页面 | 保存并回读，不创建隐式标签页或模型请求 | 已通过；注入控制器创建前的启动失败，默认模型和凭据保存成功，前后会话文件均为零，请求数不变 |
| 空闲及运行中会话 | 已有会话默认模型不变；当前工作使用旧连接，下一次使用新连接 | 已通过挂起的 HTTP 请求验证；下一请求使用已保存的新凭据。已有 flash 会话不变，新建本地会话使用 vision-exp |
| 重启与旧版本读取 | 候选和 1.38.2 均能读取保存后的设置与历史 | 已通过；两者均读取第二个连接及 17 条本地回复，1.38.2 也读取 14 条远程回复。候选修改默认模型后，已有 flash 会话不变，新建本地会话选择 vision-exp |
| Desktop 凭据代理远程会话 | 同模型的密钥版本并存，当前和下一次请求分别使用对应密钥 | 已通过真实 SSH 连接及候选 Serve 验证，挂起请求和后续请求分别使用对应凭据版本 |

最终原生请求 28–31 覆盖 SSH、本地各自的挂起请求和下一请求，分别使用对应凭据版本。无运行时夹具保存默认模型和凭据后，会话文件仍为零，请求数保持 31。最终原生 Desktop 完整测试耗时 212.733 秒并通过，此前相关测试已重复 50 次通过。原生验收发现并修复 MCP 进程上下文过早取消、仅显示用途的配置读取固定环境凭据，以及依赖时钟的提示词历史标识；对应关闭顺序、元数据和冻结时钟回归测试均通过。此前失败记录保留为诊断证据。

两个 Go 模块的完整测试、所有权竞态测试、前端完整测试与生产构建、两个 Go lint 和仓库 lint 均已通过。会话文件名在构造边界完成组件校验后，CodeQL 已通过。后续仅测试提交 `65cce8c90346f613f3a29584f694d9662b3f5a16` 为工作区锁选择规范化后不冲突的夹具；四项并行测试重复 1,000 次 race 通过，workspacelease 完整 race 测试也通过。这些测试夹具不改变已验收的产品二进制。发布仍要求最终 PR head 的 CI 全部结束并完成评审，随后通过发布候选门槛。仅文档的后续提交不改变已验收的产品二进制；任何后续产品修改均须重新评估。持久化、降级和旧 Serve 行为见[模型设置](MODEL_SETTINGS.zh-CN.md)。

## 关闭边界修复后的补充验收

候选提交的主分支 CI 发现，控制器关闭返回后，自动收件箱扫描仍可能刷新磁盘侧车。产品提交 `580caee5be5346b86188a4f0027050e6d6d68c6e` 在关闭边界等待侧车打开与扫描完成，并在进入宿主准入回调前释放该边界，使派发器仍能安全替换自己的控制器。受控测试在旧实现的 `Close` 和 `ReleaseResources` 两种模式下均失败；修复后通过 20 次 race 重复及 1,000 次陈旧前台恢复测试。根模块和 Desktop 全量测试、control/serve/sessioninbox 全量 race 与 lint 均通过。

新的 Windows 11 ARM64 生产二进制 SHA-256 为 `0671CF45DA435560E66D5E694DD10C1BB0465E868DD5673E3003C734C9DC148D`。原生关闭、自身退出与陈旧恢复测试重复 20 次通过。同一隔离安装恢复了原有 14 轮 SSH 历史和 17 轮本地历史。新的 Desktop 与 Serve 完成 SSH 请求 32；保存的一次性凭据在 SSH 请求 33 前通过运行时替换生效，本地请求 34 也使用该凭据。两次响应均完成，已有本地会话仍保留 flash 选择，原生关闭窗口后候选进程退出。前端资源与持久化格式未变，因此先前完整的升级、持有请求和无运行时验收矩阵与本次补充验收共同适用。

仅修改测试的提交 `37b20ce5706e9c3b5b87c8ebbec0c78d273712fa` 在删除 bot 测试临时目录前关闭进程级历史与用量索引。旧测试在 Windows 上复现 SQLite 文件占用；修复后 bot 全包通过 20 次原生 Windows 重复和 10 次 race 重复。该提交不改变已验收二进制。以上记录证明产品验收结果；发布完成仍以最终候选 CI、不可变标签和独立发布后核验为准。

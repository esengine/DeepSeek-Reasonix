# Release candidates

The protected `main-v2` control workflow prepares an immutable candidate after
reviewed Stable Notes are embedded. `Prepare release candidate` with `version`
builds the shared CLI/npm files, signs Desktop files, runs final-package native
acceptance, and seals a record. `Publish release candidate` accepts only that
record, checks its provenance and bytes, and requires one `release` approval
before creating the three tags and publishing. `recover` reuses the same sealed
files; it must not rebuild or sign them.

## Public-site recovery / 官网独立恢复

Candidate preparation and publication execute offline identity, resolver-output,
archive-integrity, atomic-tag, and publication-ledger contracts before expensive
work or approval. A separate bounded HTTP check reads the public Stable manifest
from the runner without credentials. It accepts the current older version before
a new release, but rejects HTTP errors, browser challenges, and malformed JSON.

候选准备与发布在昂贵任务或审批前执行离线契约测试，覆盖身份、解析器输出、归档校验、原子标签及台账。
另从 runner 以无凭据请求检查公开 Stable 清单；发新版前允许清单仍为旧版，但 HTTP 错误、浏览器挑战及
无效 JSON 必须失败。离线契约测试与在线可访问性检查相互独立。

For an already-published release whose website deployment or public verification
failed, dispatch **Recover release site** (`release-site-recovery.yml`) on
protected `main-v2` with `version=X.Y.Z`. It resolves all existing tags on protected
history, re-reads public package identities, and requires one `release` approval.
After approval it rechecks the public state, requires an existing matching release
event, and uses the same site-sync owner as full publication. No candidate artifact
is needed, so this remains usable after candidate payload expiry. It does not
build, sign, create tags, upload release assets, publish packages, or move pointers.
Missing packages or a missing release event require full publication recovery.

已发布版本若官网部署或公开验收失败，可在受保护 `main-v2` 上运行 **Recover release site**，输入
`version=X.Y.Z`。它校验三个已有标签属于受保护历史，重新读取公开包身份，并要求一次 `release`
审批。审批后再次核验公开状态及已有的匹配发布事件，复用完整发布的官网同步脚本。不依赖候选产物，
因此候选 payload 过期后仍可使用；不构建、不签名、不建标签、不上传安装包、不发布 npm、不移动指针。
缺少包或发布事件时，应使用完整发布恢复。

The workflow shares the global publication lock. A proven newer Stable pointer
preserves the newer site. Each Pages dispatch has a unique run/attempt correlation
ID, and Pages rechecks ownership immediately before deploying. The final verifier
checks the hydrated download page. A 90-day ledger records the source/control SHA,
Pages run, observed surfaces, and exact failed stage; a failed HTTP observation
never becomes a successful newer-pointer result. Recovery success is not a claim
that unavailable services were verified.

流程共用全局发布锁；只有确证存在更新 Stable 版本才保留新官网。Pages 调度带当前 run/attempt 的
唯一关联 ID，部署前再次检查版本归属，最后验证浏览器渲染后的下载页。90 天台账保留产品/控制 SHA、
Pages run、已验证渠道及失败阶段；HTTP 失败不会再被当作“新版已部署”。外部访问未恢复时不会报告验收完成。


For qualification without publication, dispatch `Prepare release candidate` on
protected `main-v2` with `version` and `rehearsal=true`. An already reviewed
version may be used for this isolated run. It uses separate
`release-candidate-rehearsal-*` artifacts, records `purpose=rehearsal`, and
cannot pass the normal publish resolver or payload verifier. The Desktop child
accepts the existing version tag only in this non-publishing mode. The run must
still complete source CI, signing, and native acceptance. It creates no tags,
GitHub Releases, npm packages, Homebrew updates, R2 pointers, or site changes.

After sealing, run `Verify release candidate rehearsal` on `main-v2` with its
candidate ID. This independent workflow downloads the exact record and payload
artifact IDs. It checks the GitHub archive digest, protected producer run,
OIDC file attestations, sealed file digests, and native acceptance receipts.
Its 90-day report binds the producer and verifier runs without compiler or
signing credentials. The verifier proves reuse of the same signed bytes; it is
not a publication or a substitute for a later formal release's public checks.

The candidate payload lasts 30 days and its record/evidence 90 days. If the
payload expires before publication, prepare a new candidate. The release
skill's public postflight remains the authority for tags, the CLI and
Desktop releases, Desktop updates, and the hydrated website after an
authorized publication.

## Tag publisher identity / 标签发布身份

Publication requires the repository secret `RELEASE_TAG_TOKEN` and variable
`RELEASE_TAG_ACTOR` (the token owner's login). Use a maintainer already allowed
by the release-tag rulesets, with repository Contents write access. Do not copy
a workstation login token or repurpose another integration's secret. Configure
this dedicated credential before enabling publication; there is no default
`GITHUB_TOKEN` fallback. The token is available only to protected `main-v2`
identity-check and activation steps, not candidate builds.

发布前需配置仓库 secret `RELEASE_TAG_TOKEN` 和变量 `RELEASE_TAG_ACTOR`（凭据所有者登录名）。
使用标签保护规则已允许的维护者身份，并授予目标仓库 Contents 写权限。不要复制工作站登录凭据，
也不要挪用其他集成的 secret。未配置时发布预检直接失败，不回退到默认 `GITHUB_TOKEN`。
该凭据只在受保护 `main-v2` 的身份检查及标签激活步骤使用，不传给候选构建。

Before approval, the workflow reads the authenticated actor, repository push
access, and inherited rulesets. Activation repeats those checks, binds the actor
ID to preflight, and uses the same credential for one atomic three-tag push.
Unknown matching rules fail closed. This is an early policy check, not proof of
every token scope or a reservation: GitHub remains authoritative at push time.
No rule is disabled, no tag is moved, and an existing complete recovery identity
is verified without pushing. Rotate the dedicated token when required.

审批前检查真实凭据身份、仓库推送权限及继承的规则集；激活前再次检查，并与预检的用户 ID 绑定。
三个标签仍以同一身份原子推送。无法判断的匹配规则按失败处理。预检不能证明全部 token scope，
也不锁定远端状态，推送时仍以 GitHub 的实时判断为准。已有完整标签的恢复只校验，不重新推送。
保护规则不变，已发布标签不移动；按需轮换专用 token。

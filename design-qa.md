---
status: active
owner: @esengine
backup: @SivanCola
reviewed: 2026-10-18
---

# Foundation palette verification

## Compact tool approval / 紧凑工具审批 · 2026-10-22

final result: passed

Scope: Studio tool approval only. The user selected the four-action mock, then
explicitly requested short labels, one explanatory line and less whitespace.
This acceptance does not supersede the unrelated preset-editor findings below.
范围：仅 Studio 工具审批；保留四种授权行为，按用户后续选择收紧文案和间距。

Review follow-up / 评审修订 (2026-10-24): integrated `studio` at `ef79a2215e`.

- The footer and receipt retain upstream's saved-with-conversation wording in both
  languages, including persistence after resume and the tool-specific destinations
  for paid search, network egress and MCP sign-in.
- Upstream wording tests are kept; three additional card-level regressions cover
  those destinations. The four short actions and original callbacks remain.
  Session grant resume/revocation tests pass.
- The October 22 screenshots below document layout only; their transient-session
  wording is superseded. A fresh 390px browser check verifies both languages without
  horizontal overflow.
- The approval browser checks pass; the same seven previously recorded broader
  output/alignment failures remain outside this change.

更新说明与回执明确授权随对话保存；保留上游语义测试及四种操作，恢复/撤销测试通过。

- Source visual truth: the selected four-action mock (2048 × 768), refined by
  the approved compact-layout instructions. Visual evidence belongs on the PR.
- Full view: `approval-compact-desktop.jpg`, 1280 × 800 CSS/image pixels, 1× density.
- Focused comparison: `approval-compact-light.jpg`, 760 × 222 image pixels;
  source and implementation opened together. Compare the component regions,
  excluding the mock's outer canvas; reduced height and shorter text are the
  approved compact refinement, not a pixel-identical recreation of the mock.
- Additional states: `approval-compact-dark.jpg`, `approval-compact-narrow.jpg`
  and `approval-compact-english-narrow.jpg`; narrow viewport 420 × 800 at 1×.
- Typography: existing Studio UI/mono families; 14px heading, 13px command and
  buttons, 12px note. Chinese and English labels remain readable without clipping.
- Layout: four right-aligned actions on desktop, two columns below 460px card
  width. Buttons are 36px tall on desktop and at least 44px on narrow layouts.
- Colors: semantic theme tokens, neutral secondary buttons, one filled primary;
  light-theme button text contrast measured 13.65:1, dark theme visually checked.
- Assets: existing product shield icon reused; no new raster or icon geometry.
- Content: session/always/deny/once retain their original verdicts and host-provided
  availability. Short Chinese/English labels and the note are presentation only.
- Interactions: all four decisions checked through the actual component in the
  isolated browser fixture; settlement receipts match each verdict. Keyboard tab
  order follows visual order. Browser console error log was empty.
- Comparison history: the initial long-label version squeezed narrow buttons into
  tall blocks; responsive layout fixed that. The approved compact refinement then
  removed the repeated intro and combined the note. Re-capture confirmed two-column
  narrow actions, no horizontal overflow, and correct light/dark hierarchy.
- No remaining P0/P1/P2 findings within this approval-card scope. No P3 follow-up.
- Checks: 490 component/i18n tests, production build, `make check`, UI census and
  its 10 frozen fixture reports passed. `perf/cards.mjs` approval checks passed;
  its seven broader tool-output/transcript-alignment failures also reproduce on
  the unchanged `a708a88a75` baseline. They are not claimed as passing.
- Limits: browser fixture, not a packaged Windows run; real permission-rule disk
  persistence was not exercised. Backend authorization code was not changed.

## Current preset editor verification / 当前预设编辑页验收

final result: blocked

This section supersedes earlier preset-card visual acceptance below. The selected
source is the user's attached `exec-e196c6a9-d8d9-437d-a6ee-0e99a3651845.png`,
1487 × 1058 image pixels; CSS viewport and density are unspecified.

The image specifies a catalog followed by a separate content editor, not a drawer. The existing
Settings shell remains the application-owned frame.

The latest editor instruction supersedes the original advanced disclosure:
the user's `codex-clipboard-c3951359-4571-4572-ad30-9f4517a2f7db.png` reference
places both textareas and the runtime-context switch directly in the editor.
Scope and ID are also exposed. Settings navigation now reads Persona / 人设.

本节取代下方旧预设卡片的视觉验收。采用用户重新指定的首张效果图，列表与独立
编辑页切换，保留现有设置外框。编辑态为新建、名称与用途已填写、能力及高级设置
收起、个人范围、取消与保存操作固定在底部。

- Implementation screenshot: unavailable. Browser acquisition of the local
  preview was rejected by the browser URL security policy. No alternative browser
  surface or raw browser command was used to bypass the refusal.
- Viewport, implementation pixel dimensions, CSS size and density normalization:
  unmeasured. Full-view and focused visual comparisons remain blocked.
- Typography: existing Studio family and weight tokens retained; rendering,
  wrapping and optical weight require browser comparison.
- Spacing/layout: separate editor, expandable rows and flex footer implemented;
  actual proportions, scrolling and narrow-screen overflow remain unverified.
- Colors: existing surface, text, focus and border tokens reused; rendered theme
  contrast remains unverified.
- Assets: existing product icons retained; no generated product assets or new
  handcrafted icon geometry. Source brand is supplied by the existing shell.
- Copy: name, purpose, persona, capabilities, scope and ID follow the selected
  references, with Chinese/English labels. Existing backend limits remain
  authoritative rather than adopting illustrative mockup character limits.
- Interaction regressions cover creation, copy, literal persona fields, invalid
  parameters, failed saves, navigation/close confirmation, pending saves and focus.
  These are jsdom checks and do not substitute for browser visual evidence.

The catalog-button follow-up uses the user's 1374 × 916 browser annotation as
the before evidence. The create button now uses one horizontal flex row, a
36px minimum height and 16px horizontal padding.

The heading uses a two-column
grid, with its description on a separate full-width row. After-render visual
verification remains blocked by the browser policy described above.

Remaining checklist:

1. Acquire the authorized preview and capture the specified creation state and catalog.
2. Compare the full view and editor regions; verify light/dark, narrow scrolling and console output.
3. Resolve P0–P2 findings before marking visual acceptance passed.

Earlier passing sections below are historical evidence for their own changes.

待办：恢复浏览器访问后，在相同编辑状态比较整页与局部，并检查深浅主题、窄屏
滚动及控制台；解决 P0–P2 问题后再将视觉验收标记为通过。

## Built-in cards and new-task default / 内置卡片与新任务默认

final result: blocked

Source: the user's selected Harness screenshot
`codex-clipboard-be3c8df5-ada5-4683-a66d-81cdf31010e7.png`, 1178 × 760 image pixels.
It is a section crop; viewport, CSS size and density are unspecified.

Implementation capture and full-view/focused comparison remain unavailable
because browser observation was rejected by the URL security policy. No
alternative browser surface was used to bypass that refusal.

- Typography: inherited Studio font, scale and weight tokens. Rendering remains
  unverified; source-specific font substitution was not introduced.
- Layout: responsive two-column cards, large rounded borders, equal-height bodies,
  separate footer actions and single-column narrow layouts. Rendered rhythm and
  scrolling remain unverified.
- Color: existing surface, border, text and selected tokens. The selected badge
  uses the text/page pair. Theme contrast still needs browser inspection.
- Assets: supplied product icon library reused for copy; no new raster assets
  or handcrafted icon geometry.
- Content: Standard/Minimal remain the shipped modes; IDs remain canonical.
  Details, usage and copy remain separate from the card's default selection.
- Behavior: card clicks persist a machine-local built-in choice. New local and
  remote requests carry it explicitly; history and explicit per-task choices
  retain priority. Old server preferences alone remain inert.

待浏览器恢复后，对照所选卡片截图检查字体、留白、颜色、图标和文案，并覆盖
深浅主题、窄屏、默认状态及展开的使用说明。当前功能测试不能替代视觉验收。

## Historical palette verification / 历史配色验收

final result: passed

Clear Blue and Tea Green implement the two approved colour directions for
[#12909](https://github.com/esengine/DeepSeek-Reasonix/issues/12909).
The existing default remains the baseline. Layout, typography, copy and brand
assets follow the product rather than generated mockup variations.

## Screenshots

Captured from the actual Studio components with synthetic performance-harness
data, one conversation turn and an empty composer. Fixture workspace and host
names are not real user data. Screenshots use 1:1 density.

| Palette | Light, 1280 × 720 | Dark, 1280 × 720 |
| --- | --- | --- |
| Default baseline | [Light](docs/design/theme-palettes/default-light-wide.jpg) | [Dark](docs/design/theme-palettes/default-dark-wide.jpg) |
| Clear Blue | [Light](docs/design/theme-palettes/clear-blue-light-wide.jpg) | [Dark](docs/design/theme-palettes/clear-blue-dark-wide.jpg) |
| Tea Green | [Light](docs/design/theme-palettes/tea-green-light-wide.jpg) | [Dark](docs/design/theme-palettes/tea-green-dark-wide.jpg) |

| Palette | Light, 390 × 844 | Dark, 390 × 844 |
| --- | --- | --- |
| Default baseline | [Light](docs/design/theme-palettes/default-light-phone.jpg) | [Dark](docs/design/theme-palettes/default-dark-phone.jpg) |
| Clear Blue | [Light](docs/design/theme-palettes/clear-blue-light-phone.jpg) | [Dark](docs/design/theme-palettes/clear-blue-dark-phone.jpg) |
| Tea Green | [Light](docs/design/theme-palettes/tea-green-light-phone.jpg) | [Dark](docs/design/theme-palettes/tea-green-dark-phone.jpg) |

## Visual comparison

The approved light mockups were 1619 × 971 pixels, including a caption outside
the application. They were viewed together with the implementation screenshots.
Comparison excluded that caption and assessed the palette direction, rather
than treating generated layout drift as a product specification.

- Typography: existing families, size, line height and tracking retained. Plain
  light palettes now share the default prose weight; user overrides still win.
- Spacing: existing grid, controls, corners and panel proportions retained.
- Colours: cool white/blue and warm white/tea-green surfaces match the approved
  directions. Text, syntax and button contrast checks pass in both schemes.
- Assets: existing logos and icons retained; no added image or sky layers.
- Copy: existing workbench text retained; new palette names and descriptions
  have Chinese and English labels.

Full views made the navigation, transcript and composer readable without
additional crops. Dark palettes extend the approved hues; the source mockups
did not specify a dark layout. Phone captures confirm no horizontal overflow.

## Findings and iteration history

1. Default preview inherited the selected pack, and plain packs changed reading
   surfaces and weight. Shared canonical default variables and plain-surface
   rules now keep the preview independent and typography consistent.
2. Initial comparison found neutral selected navigation and a dark send button
   overridden by body colour. Shared pack selectors now use accent ink/wash and
   the accent/foreground pair; disabled controls remain neutral.
3. Revised screenshots were compared with both approved directions. No
   actionable P0/P1/P2 differences remain. Sky themes now join image themes in
   the expandable collection; the active backdrop remains in the first group.

## Behavioural evidence

- Immediate selection, return to default, light/dark and system-following modes
  verified in the browser; each preview follows its own scheme tokens.
- Default comparison: original and revised Studio CSS applied to the same DOM.
  Fourteen style properties matched for 735 visible elements in each scheme
  under a light system and 734 in each scheme under a dark system.
- Compared colour, background, border, shadow, font, size, weight, line height,
  tracking, width, height, padding, margin and radius.
- Go integration tests cover selection, config persistence, server recreation
  and reset to the default for both new built-in IDs.
- Frontend neighbourhood: 22 test files, 209 tests passed; production build and
  typecheck passed. Browser warning/error logs were empty.
- Theme and HTTP server package tests, repository checks and whitespace checks
  passed. Lint budgets were not increased.

## Remaining coverage

Windows native installer acceptance and hosted CI are separate from these
local results. The production build retains its existing large-chunk warning.

# Composer composition placement / 输入框组合布局

final result: passed

The annotated request moves the existing composition selector and management
entry beside the run status. It preserves the existing screen, visual tokens
and session behavior. / 按标注将组合选择与管理入口移到运行状态旁，沿用现有界面。

## Evidence / 证据

- Source visual truth: author-approved composer annotation (1310 × 1826 pixels; browser chrome included, density unspecified). The user attachment remains private; the synthetic baseline below records the same controls.
- Matched source capture: `docs/images/agent-compositions/composer-before-light-wide.png`.
- Implementation: `docs/images/agent-compositions/composer-light-wide.png`.
- Full-view comparison: the matched before/after captures above were opened together, each 1440 × 1000 pixels, 1440 × 1000 CSS px, device scale factor 1. No density resampling.
- Focused comparison: composer controls from those captures were compared together at original pixel density; `composer-context-light.png` records the final control arrangement.
- Additional captures: `composer-light-390.png`, `composer-dark-390.png` (390 × 844 CSS/pixel dimensions), and `composer-dark-wide.png` (1440 × 1000), all in the same image directory.
- State: local Mock preview, Chinese, idle, General selected, existing input draft and missing-key notice. No provider request submitted.

## Findings / 结论

No actionable P0/P1/P2 differences remain. / 无需修复的 P0/P1/P2 差异。

- Typography: existing UI family, 11 px medium-weight controls and status typography retained; long names remain bounded by the selector.
- Layout: selector begins 16 px after run status; controls occupy the previously unused top row. The bottom toolbar fits one row at 1440 px. At 390 px the selector, status and management entry remain visible, with document width exactly 390 px.
- Colors: existing semantic theme tokens and hover/focus styles reused in both light and dark mode; no new palette.
- Assets: logo and icons preserved; no generated or replacement imagery.
- Content: composition names, source labels, error notice and management action retained. The placement is the only intentional visual change.
- Accessibility: composition controls sit outside `role="status"` and its live announcements. Accessible names and existing lock behavior are retained.

## Checks / 验证

- Switched General → Code review → General through the relocated selector.
- Opened Agent presets through the relocated management entry, then closed it.
- Confirmed the selector is absent from the bottom toolbar in the real DOM.
- Browser warning/error log after interaction: empty.
- Typecheck and `make check`: passed.
- Focused interaction tests: 92 passed; full frontend suite: 632 files, 5,220 tests passed.
- Locked-session/new-session flow covered by the existing picker interaction test.

## Comparison history / 对照记录

First rendered comparison passed; no P0/P1/P2 visual repair iteration was needed.
The matched before/after captures demonstrate the requested move and reduced
bottom-toolbar wrapping. / 首轮对照通过，上移后底栏在宽屏减少一行。

## Limits and next check / 范围

Browser-owned layout only; native titlebar and shell behavior are unchanged and
were not re-tested. No provider execution or new CI result is claimed for this
layout edit. / 本次验证浏览器布局，不涉及原生窗口或模型执行。

Checklist complete: placement, selection, management, live-region separation,
light/dark, wide/narrow, typecheck, affected and full frontend tests.

# Unified composition menu / 统一组合菜单

final result: passed

This section supersedes the separate selector/management controls above.
The annotation asks to combine those entries without changing the surrounding
composer. / 本节替代上述独立选择器与管理按钮，按标注合并为一个入口。

## Source and capture / 来源与截图

- Source visual truth: author-approved annotation showing the composition selector and adjacent management button, 452 × 312 pixels; density and full viewport unspecified. The user attachment remains private.
- Current-run baseline: `docs/images/agent-compositions/composition-menu-before.png`, 1440 × 1000 CSS/pixel dimensions, scale factor 1.
- Implementation: `composition-menu-light-wide.png`, `composition-menu-dark-wide.png` (1440 × 1000) and `composition-menu-light-390.png`, `composition-menu-dark-390.png` (390 × 844), all in the same directory, scale factor 1.
- Focused implementation: `composition-menu-detail.png`, 344 × 364 pixels at scale factor 1, including menu and trigger.
- Full-view comparison: `composition-menu-before.png` and `composition-menu-light-wide.png` were opened together. Baseline is closed; implementation is open to assess the added menu and unchanged surrounding composer.
- Focused comparison: the annotation and `composition-menu-detail.png` were opened together without density resampling. Source typography cannot support a 1:1 pixel comparison; the matched viewport supplies the product typography/layout reference.
- State: local Mock preview, Chinese, idle, Code review selected. No provider execution.

## Flow audit / 流程检查

1. Read the active composition: healthy after combining the entries. A single trigger shows name and localized source. The separate ellipsis had no visible explanation; its action is now explicit.
2. Choose a composition: healthy. Menu rows show capability notes and source, mark the current choice, and close after selection. Evidence: `composition-menu-light-wide.png` and `composition-menu-selected.png`.
3. Manage presets: healthy. A separated footer opens the existing management page. Evidence: `composition-menu-manage.png`. Management costs one extra click; frequent switching retains the same click count.

## Fidelity and interaction / 视觉与交互

- Typography: existing UI family and weights; 13 px choice titles and 11 px notes. Source labels sit beside names rather than consuming another row.
- Spacing: existing composer-menu recipe, 320 px menu width, 12 px radius and 8 px inner padding. At 390 px, the menu spans x=64…384 and the document remains exactly 390 px wide.
- Colors: existing surface, overlay, text, muted and focus tokens work in both themes; no new palette.
- Assets: existing chevron and brand assets retained; no new image assets.
- Copy: localized Built in/Personal/Project labels replace raw source identifiers. Management is a named footer action.
- Accessibility: trigger name includes the active composition and lock state; selected rows expose current state. Keyboard arrows skip disabled rows; Enter selects, Escape closes and restores trigger focus. This is scoped interaction evidence, not a full accessibility compliance claim.
- Safety of interaction: started/busy conversations never rebind through this menu. The same menu offers explicitly labeled new-conversation actions and management; hosts without creation support still expose management.

## Iteration and validation / 迭代与验证

- First inspection found source metadata on an extra line, making each choice unnecessarily tall. The item title now uses a flex row; revised captures show compact name/source pairs. No actionable P0/P1/P2 visual issues remain.
- A later resize check found a P2 shared positioning defect: after 390 → 1440 px, menu left remained 88 px while its trigger settled at 570 px. Window resize preceded fold publication and sidebar transition completion.
- Shared floating placement now responds to fold publication and transition completion. The revised dark-wide capture and browser rectangles show both left edges at 570 px.
- A deterministic real-Picker regression fails without the positioning fix and also checks cleanup after closing.
- Verified selection, management, Escape and responsive placement in the browser; warning/error log was empty.
- Regression tests cover keyboard focus, distinct action identities, failed/pending selections, locked sessions, management without creation support, equal names from different sources, and deleted bound presets.
- Final validation after the resize repair: typecheck passed; frontend full suite passed, 633 files and 5,225 tests. Repository checks passed without increasing a budget.
- Native shell behavior and provider execution are outside this layout change; hosted CI and publication status are reported separately in the pull request.

Checklist complete: one entry, grouped choices/actions, clear current state,
retained locks, keyboard access, themes, narrow viewport and regression checks.

## Studio integration verification / Studio 合并验证

The upstream integration preserves the top-row Agent composition trigger and
the unified selection/management menu. The upstream screen-reader-only run
label remains inside the live status region; interactive controls remain
outside it. Composition menu text follows the shared text-size setting.

合并后保留顶部 Agent 组合入口与统一的选择、管理菜单，同时沿用上游运行状态的
读屏处理。浏览器复核覆盖组合切换、管理入口，以及菜单展开时从宽屏缩至窄屏
的定位与换行。完整前端测试通过：651 个文件、5,342 个用例。

| Browser | Light | Dark |
| --- | --- | --- |
| 1440 × 1000 | Capture (local capture) | Capture (local capture) |
| 390 × 844 | Capture (local capture) | Capture (local capture) |

These captures use synthetic Mock data. Native Electron shell behavior was
not requalified by this browser integration check.

## Standard and Minimal modes / 标准与精简模式

The management page references the supplied Harness card hierarchy while using
Studio typography and surface tokens. Only Standard and Minimal are shipped;
custom definitions remain separate. Standard retains its existing stable ID.
Minimal uses native terminal/image tools and compaction without Cordis plugins.

管理页保留内置卡片、默认标记、模式说明与独立自定义区域。宽屏并排，390px
窄屏纵向排列且可滚动。默认选择与当前会话绑定分离；复制后继续使用原编辑器。
入口只展示模式名和箭头，位于左侧；运行状态与 token 位于右侧，来源与锁定
信息保留在菜单、提示和无障碍名称中。

| Browser | Light | Dark |
| --- | --- | --- |
| 1440 × 1000 | Cards (local capture) | Cards (local capture) |
| 390 × 844 | Cards (local capture) | Cards (local capture) |

Composer idle (local capture) and
running (local capture) captures
show the compact mode trigger with separate run information. These are browser
Mock captures; the source annotation image is not published.

The full frontend suite passes 651 files and 5,343 tests. Mechanism checks retain
explicit custom read-only fixtures for proxy, late-registration and child-agent
ceilings. Minimal's terminal is intentionally allowed. Text-only models report
an unavailable image dependency instead of silently changing the tool set.

After the subsequent MCP-follow integration, the full frontend suite passes
651 files and 5,347 tests. Provider-boundary and race regressions confirm that
automatic server-tool approval cannot replace an existing composition's pinned
contract; a new conversation can bind the updated catalog.

The native-video integration passes 651 frontend files and 5,348 tests. Combined
projection regressions retain bindings/video references in stored history while
removing local binding metadata and answered video payloads from model requests.

## Workspace preparation / 工作目录准备

The workspace palette and preparation states reuse existing typography and
tokens. Real Git checks cover branches, creation, retry and cancellation; visual
QA passed. Captures remain in
[PR #13598](https://github.com/esengine/DeepSeek-Reasonix/pull/13598).

工作目录面板与准备状态复用现有字号、图标和表面样式；真实 Git 验证了分支搜索、
占用状态、路径展开、创建、成功、失败重试与取消。无残留的 P0/P1/P2 视觉问题，
截图仅保留于 PR。清理失败时保留外部修改，并明确提示保存任务、核对目录后关闭
会话，从源目录新建会话重试；安装包与上游 CI 验收单独记录。


## Maintainer opt-in revision / 默认关闭调整

Standard and Minimal cards, the left-side name-only picker and right-side run
information are preserved. Preferences are labeled as preferences; they do not
activate a mode. No selection and Standard remain unbound. Complete replacement
is removed from the editor; selected personas supplement first-party guidance.

保留标准／精简卡片、左侧仅模式名的入口及右侧运行信息。首选项不自动启用；
未选择及标准模式不绑定。编辑器移除完整替换开关，人设仅补充内置指令。

Per maintainer request, composition screenshots are local evidence and are not
committed. Historical capture references above describe earlier verification.
Current browser checks and hosted workflow results are reported separately.

按维护者要求，组合截图仅保留为本地证据，不提交到仓库。上方记录为历史验证；
当前浏览器检查与远端工作流结论分开报告。

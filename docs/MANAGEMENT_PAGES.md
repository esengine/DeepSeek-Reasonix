# Full-window management pages

Trash, Automation and Settings retain independent entries and share `ManagementPageShell`. They fill the application window without changing native fullscreen, maximize or close behavior. Each feature loads lazily; a failed chunk can be retried or exited.

## Navigation and workspace

`useAppNavigationStore` owns workspace/settings/trash/automation navigation, the last settings category, focus restoration and the single automation-conversation return target. The legacy providers category resolves to models. Transient menus and dialogs remain independent overlays.

The workspace stays mounted at its existing dimensions and becomes inert while a management page is active. Returning does not reactivate the current conversation, reload history, or introduce another transcript scroll writer. Background work continues. Entry focuses Back to workspace; exit restores the invoking entry. Escape closes child menus/dialogs, while the close-tab shortcut returns to the workspace. Hidden workspace shortcuts are suspended; command palette, settings and explicit conversation navigation remain available.

Opening an automation's linked conversation uses the existing guarded navigation queue. Success exposes Back to automation; failure keeps the editor visible. A newer navigation invalidates earlier results. Deliberate project/conversation changes clear the temporary return target.

## Trash

The wide layout places search, scope/date filters and grouped conversations on the left, with a read-only preview on the right. System recovery copies remain separately collapsed. Normal history keeps its existing dialog and shares the list/preview implementation.

Selection uses stable paths and advances to the nearest remaining row after a mutation. Preview request generations prevent late responses from replacing a newer selection. Loading failures are distinct from empty results. Restore and purge have explicit asynchronous outcomes.

Permanent deletion requires confirmation with initial focus on Cancel. Empty Trash snapshots every ordinary deleted conversation, including filtered-out rows, and excludes system recovery copies. The batch runs sequentially, continues after individual failures, reports totals and offers an explicit failed-only retry. A successful mutation followed by a failed refresh is reported separately, so successful destructive operations are not resubmitted.

## Automation drafts

`useAutomationDraftStore` keeps per-ID baselines, editable values, frequency choice, detail tab, conflicts and operation versions in memory for the application run. Switching tasks, filters, pages, detail visibility and linked conversations preserves drafts. New unsaved tasks remain discoverable; changed existing tasks show an Unsaved badge. Reloading the webview or exiting the process clears drafts; minimize/tray hiding does not.

Only Save commits scheduling configuration. Failed saves preserve input. A pending operation locks its task without preventing navigation to another task; completion never steals selection. Discard restores the latest baseline or removes an unsaved new task. Run now uses saved configuration, and enable/pause updates only enabled. Confirmed deletion stops future scheduling while retaining existing conversations.

The existing serial mutation queue and revision/etag checks remain. Engine-owned run fields merge without losing edits; disjoint external configuration changes merge; conflicting fields block normal saving and offer Reload latest or Save as a new paused task. Externally deleted tasks retain recoverable drafts without resurrecting the original ID. New-task ID collisions allocate another ID. Results are bound to task identity and operation version.

## Layout and compatibility

The shared titlebar reserves 44px on macOS and 48px on Windows, with existing 46px Windows controls. Stable chrome is draggable; controls/content are not. At 960px and above, lists and details use two columns (minimum 280px and 420px). Narrow windows switch between list and detail without losing input. Trash defaults to a 34% list; Automation restores a valid width preference or uses 40%. Content scrolls independently and primary actions remain visible.

New copy in `managementLocale.ts` covers Simplified Chinese, Traditional Chinese and English. Existing theme tokens provide light/dark styling. No configuration migration or model/prompt/tool/cache-protocol changes are required.

## Verification (2026-09-05)

Deterministic tests cover navigation generations, draft merging/conflicts/deletion, switching tasks during save, failures, keyboard isolation, preview races, recovery-copy exclusion, partial deletion and post-success refresh failure. All 264 frontend suites passed, including heartbeat/history/settings; the separate transcript regression command also passed. Type checking and Hooks/CSS/layer/theme/WAAPI/single-scroll-writer checks run with the production build.

Windows browser preview was exercised at 1280×900 and 800×800, including model settings, trash confirmation/cancellation, and draft/detail retention. No real conversations were purged and no automation task was executed. An isolated macOS native build was checked for page entry, Escape, Command-W return, minimize/restore and window zoom. Windows ARM64 built and launched with isolated data in Win11, displaying onboarding. Native Windows interactions and 100%/125%/150% DPI checks remain unverified; browser preview is not a substitute.

The initial payload measures approximately 465.4 KiB gzip JavaScript and 2480.9 KiB raw JavaScript/CSS, versus 464.7/2481.7 KiB on the integrated main-v2 base. The gzip ceiling is 465.5 KiB; the upstream raw ceiling remains 2481.7 KiB. The Traditional Chinese chunk rounds up to a 61.7 KiB ceiling. Feature bodies remain lazy and other budgets remain enforced.

The integrated titlebar dispatch was checked in an isolated macOS native build: double-click maximizes and a second double-click restores the original window size. Unsaved credential discovery uses a transient capability resolver and cannot pollute the saved capability cache.

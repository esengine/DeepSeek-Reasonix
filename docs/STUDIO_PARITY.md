---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-08
---

# Studio parity with the 1.x desktop app

## Purpose

This table lists what the 1.x desktop app (`desktop/frontend` on `main-v2`) offers and where Studio (`desktop/frontend-next`) stands.

1. The PR that closes a Missing row MUST move it to Have or Have differently in the same change.
2. A UI PR that removes or moves a visible control MUST update its row, or add one, in the same change.
3. A row marked Unverified MUST be checked against the code before it is relied on.
4. Held by review, not by a gate: no check reads this file.

Status values:

- **Have**: same capability, same place.
- **Have differently**: capability present, shape differs.
- **Missing**: absent in Studio.
- **Removed on purpose**: decided, with the reason.
- **Unverified**: not checked in the code.

Checked against `studio` at `d779d89bc` on 2026-10-08.
"No issue yet" means no open issue was found by title search.

## Reference: sessions and sidebar

| Feature | Status | Studio location or tracking |
| --- | --- | --- |
| Pin a session | Have | `ui/Workspaces.tsx` (`session.pin`) |
| Archive and restore a session | Have | `ui/Sidebar.tsx`, `ui/Workspaces.tsx`; the quick row action is #12318 (open PR) |
| Recycle bin page for deleted sessions | Unverified | Studio has "delete session" in `ui/Workspaces.tsx`; no recycle-bin page was found, recovery not checked |
| Manual rename | Have | `renameSession` in `ui/App.tsx` |
| AI rename of a session | Missing | #11570 |
| Manual order of projects and sessions | Missing | #11420 |
| Project-name context menu | Missing | #12089; #12324 (open PR) |
| Search and reference past conversations | Have differently | Rail search in `ui/railsearch.tsx`; no "reference a past session" action; #11942 |
| Export a session | Have | `exportSession` in `port/hub.ts` |
| Session recovery versions dialog | Unverified | Not searched by name; see #12365 for the delete behaviour |
| Remote (SSH) workspaces | Have | `ui/Remotes.tsx`, `ui/RemoteHosts.tsx` |
| Session takeover when another window holds it | Have | `ui/App.tsx`, `ui/Pane.tsx` |

## Reference: composer and transcript

| Feature | Status | Studio location or tracking |
| --- | --- | --- |
| Skill invocation shown as a badge in the transcript | Have differently | Composer chips in `ui/skillchips.ts`; the transcript shows plain `/name` text; #12383 (#11145 closed) |
| Slash and `@` completion | Have | `ui/Completion.tsx` |
| "Add to chat" bar on selected transcript text | Missing | #11954 |
| Find in the conversation | Have | `ui/Find.tsx` |
| Turn navigator | Have differently | Scroll rail with marks in `ui/Transcript.tsx` |
| Model picker with vendor icons, search and favourites | Missing | #12354, #11403 |
| Reasoning effort picker | Have differently | Names plus one-line descriptions, only levels the model supports; do not revert to raw `high` and `max` |
| Approval card offering "allow matching actions this session" | Unverified | Mock fixture shows two buttons only; #12020 (closed) |
| Plan and todo panel | Have | `ui/Plan.tsx` |
| Subagents panel | Have differently | `ui/Pane.tsx`, `ui/delegation.ts`; running-state gaps in #12410, #12284 |
| Goal mode controls | Unverified | Studio renders the `update_goal` tool in `ui/Sym.tsx`; the 1.x lifecycle actions were not compared |
| Mermaid diagrams in Markdown | Missing | No `mermaid` dependency in `desktop/frontend-next/package.json`; no issue yet |
| Image viewer for attachments | Unverified | Images render in `ui/Markdown.tsx`; a zoom viewer was not found or checked |

## Reference: shell, status and settings

| Feature | Status | Studio location or tracking |
| --- | --- | --- |
| Command palette | Have | `ui/Palette.tsx` |
| Completion sound | Missing | #11876; no audio call in `desktop/frontend-next/src` or `desktop/electron/src` |
| Soft Aurora colour style | Missing | #11680; the kernel still accepts `theme_style="aurora"` |
| Theme gallery and import | Have differently | `ui/Appearance.tsx`, theme packs in `docs/THEME_PACK.md` |
| Interface font size | Have differently | Whole-interface scale in `ui/Appearance.tsx`; default text is 13 px against 14 px in 1.x |
| Content width (standard or full) | Have differently | Reading width or fill window; the composer does not follow the fill; #12390 |
| Display currency | Have differently | `ui/DisplayCurrency.tsx`; not on the first settings screen; #11943 (closed) |
| Configurable bottom status bar | Have differently | Fixed run strip in `ui/RuntimeBar.tsx`; no item configuration found |
| Context window card | Have differently | `ui/ContextSummaryCard.tsx`, `ui/MeterRail.tsx`; the 1.x overview panel is not open by default |
| Usage statistics | Have | `ui/ModelUsage.tsx` |
| Model service management layout | Have differently | Settings provider list and form; #10761 (closed) |
| Close to tray | Have | `ui/WindowSection.tsx` |
| Update notes in the app | Have differently | `ui/Versions.tsx`; release notes mirrored by #12406 |
| First-run setup | Have differently | `ui/Welcome.tsx` wizard; clipped controls on wide windows in #11561 (closed) |
| Memory page with per-type colour | Unverified | `ui/Memory.tsx` exists; colouring was not compared; #11398 (closed) |
| Browser panel | Have | `ui/BrowserPanel.tsx` |
| Integrated terminal panel | Unverified | `ui/Shell.tsx` only chooses the shell; no terminal view was found by name; no issue yet |
| Worktree merge dialog | Unverified | Worktree handling exists in `ui/Workspaces.tsx`; the merge flow was not checked |
| Scheduled tasks (heartbeat) | Missing | #11384 |
| Keyboard shortcuts cheat sheet | Unverified | Key handling is in `ui/keys.ts`, `ui/windowkeys.ts`; no cheat sheet found by name; no issue yet |
| CSV and media preview in the workspace panel | Unverified | `ui/WorkbenchPanel.tsx` and `ui/CodeEditor.tsx` exist; file kinds not checked |
| IM gateway (WeChat, Feishu, QQ) | Removed on purpose | Commit `f352dfbec` "remove the IM bot gateway"; `docs/MIGRATING.md` keeps the `[bot]` section untouched; #11952 asks for it back |

## Rows added by Studio

| Feature | Status | Studio location |
| --- | --- | --- |
| Icon rail with three display modes | Have | #12377, #12428; sidebar footer entries appear only when icon navigation is hidden (#12478) |
| Usage, feedback, account and settings footer / 用量、反馈、账号与设置底部入口 | Have differently | `ui/Nav.tsx` when navigation is visible; `ui/Sidebar.tsx` otherwise, including the narrow-window drawer. 导航可见时使用图标栏入口，隐藏时保留侧栏入口 (#12478) |
| Completed-unread marker on sessions | Have | #12218 |

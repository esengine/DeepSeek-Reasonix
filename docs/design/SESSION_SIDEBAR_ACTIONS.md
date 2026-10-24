---
owner: @Oliver-Rui
status: proposed
---

# Session sidebar actions

Status: proposed. Nothing in this document is shipped unless §9 says so. It lives under `docs/design/` so the product-doc retrieval corpus (`docs/*.md`) cannot present it to a model as a feature.

Reference: the conversation-row context menu of MiMo Desktop, observed 2026-10-24. The action set and its rationale below are read off that surface; the implementation model is inferred from observable behaviour, not from source. The mapping onto this repository in §7 is grounded in source.

## 1. Purpose

A session list answers "where is my work". The row context menu answers the other question: "what can I do to it". Every verb that applies to a conversation *as an object* — retitle it, re-home it, finish with it, get it out of the way, or get rid of it — needs exactly one home, and the row menu is that home. Primary verbs (open, message, search) stay on the main surface; the menu carries the rest at zero persistent space cost, which matters because the sidebar's vertical space is its scarcest resource.

| ID | Principle |
| --- | --- |
| P1 | Each action is one kernel-owned fact. The menu renders what is currently possible; it never keeps a second copy of state. |
| P2 | Every write is durable intent through a canonical route. The renderer mutates nothing. |
| P3 | Actions are grouped by blast radius, harmless first, destructive last, alone, and confirmed. |
| P4 | OS capability (clipboard, file dialogs) belongs to the shell's main process with a verified sender, never to the renderer. |
| P5 | Batch is a mode, not a menu: entering it changes the list itself, and the same per-item verbs apply to the selection. |
| P6 | The menu grows only by subtraction pressure — a verb earns its place by being about the session as an object, not its content. |

## 2. The action set

Ten actions on a conversation row, in the order the surface presents them:

| # | Action | Does | Blast radius |
| --- | --- | --- | --- |
| 1 | Rename | retitle the session | harmless |
| 2 | Pin | hold the session at the top of the list | harmless |
| 3 | Mark unread | restore the unread fact on the session | harmless |
| 4 | Continue in new conversation | start a new session seeded from this one | structural |
| 5 | Move to project | assign the session to a project; submenu offers remove-from-project, each project, and new project | structural |
| 6 | Archive | leave the active list, keep the record | structural |
| 7 | Batch manage | multi-select mode over the list | structural (mode) |
| 8 | Copy working directory | put the session's workspace path on the clipboard | OS capability |
| 9 | Export conversation log | write the transcript to a file the user picks | OS capability |
| 10 | Delete | remove the session and its record | destructive |

## 3. Why each one exists

**Rename.** A session's subject drifts from its first message within minutes; the title is the retrieval key for every later visit. Without rename, the list fills with rows named after whatever the user happened to type first.

**Pin.** A handful of sessions are the daily drivers. Pinning is the cheapest possible answer to "the top of my list should be my work, not my history" — and order is a fact the kernel already keeps and that survives restarts, so pinning is a write into state that exists.

**Mark unread.** "I will come back to this" is a decision the *user* makes, not an arrival event. Unread is already a kernel-owned fact (drawn as the row-end dot, cleared by viewing); this action is its inverse write. Without it, the only way to make a session wait is to not look at it.

**Continue in new conversation.** The most common branch point in agent work is "same context, different task". Without a fork, users hand-copy transcripts, and the copy silently loses the lineage that says where the new session came from.

**Move to project.** Projects are the only grouping with durable meaning — a session's project is where it will be looked for tomorrow. The submenu doubles as the project surface (remove / pick / create), so one menu answers "where does this live" without a trip through settings.

**Archive.** Done is not deleted. The active list has to stay short, and the record has to survive; archive is the state that makes both true at once. It is also the honest answer to the filter that already reads "archived".

**Batch manage.** Cleanup is a batch task, and one-row-at-a-time is exactly why cleanup never happens. Entering the mode changes the list; the same verbs apply to the selection, so nothing new has to be learned per action.

**Copy working directory.** The workspace path is needed in a terminal every few minutes. Hunting for it by hand breaks flow for a fact the kernel already holds.

**Export conversation log.** Sessions outlive tasks: the record gets attached to reports, issues and reviews. Export is a canonical read of a log that already exists plus one file dialog.

**Delete.** A list needs a floor as well as a ceiling. Deletion is durable and irreversible, which is why it sits last, alone, in red, behind a confirmation.

## 4. The state model

Per `docs/STUDIO_SHELL_BOUNDARIES.md` §6, each action is classified before its API is chosen:

| Kind | Actions | Carried by |
| --- | --- | --- |
| Durable intent | rename, pin, mark unread, continue, move to project, archive, delete | canonical read/write; the kernel is the only writer |
| Projection | the menu itself: which items are enabled, what the project submenu lists | pull, recomputed per open |
| OS capability | copy working directory, export | the shell's main process, sender-verified |

No action is a notification. A delete that a closed pane missed is recovered by reading the list again, not by replaying an event — which is why the menu opens from a fresh read every time.

## 5. Composition rules

- **Order is the safety property.** Harmless first, structural next, batch, destructive last. The destructive item is furthest from where the cursor rests and is the only red entry.
- **The menu is a projection.** A session that is not in a project offers no "remove from project"; a pinned session offers "unpin". A menu that shows an impossible action is a menu that lies about state.
- **One fact per action.** Nothing in the menu performs two writes; batch is the mode that repeats one write across a selection, never a different verb.
- **Confirmation names the object.** Delete names the session; batch delete names the count. Nothing destructive is a single click.

## 6. What it deliberately does not carry

Verbs about session *content* — approve, steer, cancel, answer a question card — are not here; they already have surfaces closer to the work. The menu is for the session as an object. A menu that grows everything is a menu nobody reads.

## 7. Mapping onto Studio

`desktop/frontend-next` renders it; `internal/frontend/serve` owns the facts. Existing routes cover more of this than expected:

| Action | Route | State |
| --- | --- | --- |
| Rename | `POST /sessions/rename` | exists |
| Pin | order is already kernel-kept and restart-surviving; expose a write for it | proposed: session update carries order |
| Mark unread | inverse of `POST /sessions/viewed` | proposed: the viewed fact gains a false write |
| Continue in new | new session with a lineage field | proposed: `POST /sessions/{id}/continue` |
| Move to project | project membership beside the existing project grouping | proposed: `POST /sessions/{id}/project` |
| Archive | the archived filter already reads; this is the write | proposed: `POST /sessions/{id}/archive` |
| Batch manage | client mode looping the same per-item routes — no batch route | none needed |
| Copy working directory | shell capability; the main process already owns the platform opener and dialogs | none |
| Export conversation log | canonical read of the session log + the shell's save dialog | none |
| Delete | durable, destructive | proposed: `DELETE /sessions/{id}`, confirmed |

Unread state reuses the fact the kernel already owns (the row-end dot in `src/styles/LAYOUT.md`); relay-free, no second copy.

## 8. Security boundary

- All writes sit on the existing mux, inside the existing loopback gate — per-launch credential, exact `Host`, exact `Origin` on writes. Nothing new to build.
- The renderer never holds a credential: clipboard and file writes go through the main process with a verified sender, as every other OS-capability verb already does.
- Delete is durable and irreversible; the confirmation names the session, and batch delete names the count because it multiplies the blast radius by the selection.
- No action grants authority. Renaming, moving or archiving a session changes where work is found, never what a session may do.

## 9. Open questions

1. Pin versus project: when both are present, which orders the list?
2. Continue in new: copy the transcript, or seed from compacted context and keep a lineage pointer?
3. Archive: a state flag on the session, or a separate store with its own retention?
4. Delete: remove outright, or archive-then-purge with a window?

## 10. Stages

| Stage | Content | Acceptance |
| --- | --- | --- |
| M1 | the four harmless actions (rename, pin, mark unread, copy path) | no new concepts; pin writes into the existing order; unread reuses the existing fact and its dot |
| M2 | the structural three (continue, move to project, archive) | lineage and project membership are kernel fields; the submenu reads projects from the kernel |
| M3 | batch mode and delete | batch loops per-item routes; delete and batch delete confirm with the session name and the count |

Nothing in this document is a commitment; it is the shape a review can argue with.

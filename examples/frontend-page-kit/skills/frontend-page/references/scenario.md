---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Local frontend scenario

| Item | Requirement |
| --- | --- |
| Target | A temporary workspace containing copies of this skill's `fixture/brief.md` and `fixture/tickets.json`. |
| Input | An original fictional work-queue brief and four sample tickets. These are task inputs, not a shipped page. |
| Output | `index.html`, `app.css`, and `app.js`; use a local static server without a new dependency or backend. |
| Scope | Implement the page in the selected workspace, leaving the bundled source inputs unchanged. |
| Delivery | A local page, observed acceptance, screenshots, remaining issues, and resource cleanup using `delivery.md`. |

1. Read both copied inputs. Use their actual ticket text and state; keep the
   fictional nature visible. Follow every requirement in `brief.md`.
2. Check the page at 1280 by 800 and 900 by 700. Record readability, wrapping,
   focus, and any horizontal overflow at those desktop sizes.
3. Verify the initial four tickets, three open and one complete. Exercise all
   filters and completion controls; verify visible rows and counts agree.
4. Use only the keyboard to add a ticket, filter the queue, and toggle its
   completion. Verify accessible names, visible focus, and rejection of an
   empty title. Confirm refresh restores the supplied sample data.
5. In the temporary workspace, save a backup of `tickets.json`, replace it
   with `[]`, and reload to check the empty state. Restore the backup afterward.
6. Temporarily move that same input aside and reload to check failed reading.
   Restore it and use the page's Retry control to verify recovery. Record
   actual results; do not claim these states were tested from source inspection.
7. Preserve wanted output, restore temporary inputs, and stop only the owned
   server. Close the task's pages or isolated sessions and confirm cleanup.

If a browser or another prerequisite is missing, the corresponding acceptance
remains not run. Installing this package does not settle the page's quality.

A [worked reference](../../../example-output/delivery.md) records one local
execution of this scenario. Its output lives outside `fixture/` and does not
replace the supplied inputs or the selected model's own acceptance checks.

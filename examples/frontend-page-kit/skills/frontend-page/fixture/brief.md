---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Fictional work queue

Build a local desktop page for a small team to triage the supplied fictional
tickets. Keep it calm and readable, with the queue as the main content. The
sample is an exercise, not a report about a real product or customer.

## Content and behavior

1. Read `tickets.json` from the local static server. Show each ticket's stable
   id, title, summary, priority, and completion state. Preserve its source text.
2. Provide All, Open, and Complete filters. Show counts derived from the
   current in-memory tickets, and distinguish an empty queue from no matches.
3. Let the user toggle each ticket's completion and add a ticket with a
   title. A new ticket starts open with normal priority and a unique local id.
4. Reject a blank or whitespace-only title with an accessible message, keep
   focus at the input, and do not add a row. After a valid addition, clear the
   input and make the new ticket visible without moving focus unexpectedly.
5. Changes last only in this page's memory. Refresh reads the sample again.
   Do not add local storage, a backend, account features, or analytics.

## States and accessibility

1. Show a reading state while loading, an empty state for `[]`, and a useful
   failed-read state with Retry. Retrying after the file is restored loads it.
2. Use native controls with accessible names. All filters, additions, and
   completion changes work by keyboard with visible focus.
3. Keep long titles and summaries readable at 1280 by 800 and 900 by 700.
   Avoid horizontal page scrolling and do not rely on color to convey state.
4. Make counts, errors, and state changes understandable to assistive tools.
   State what was actually tested instead of claiming unsupported coverage.

## Output and acceptance

Produce `index.html`, `app.css`, and `app.js` without installing a dependency.
Use a server bound to loopback and the available browser. Complete the
bundled scenario's checks and delivery record. Do not deploy this exercise.

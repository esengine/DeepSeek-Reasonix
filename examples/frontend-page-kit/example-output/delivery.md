---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-06
---

# Worked local frontend delivery

This reference was built by Codex after reading the instructions and inputs
from an actual isolated installation of `frontend-page-kit`. It demonstrates
one bounded delivery, not native Reasonix live-model execution or a
community submission.

The original fixture remains a brief and four tickets only.

## Delivered page

The generated `index.html`, `app.css`, and `app.js` implement the fictional
queue from `skills/frontend-page/fixture/brief.md`. They read a sibling
`tickets.json` over a loopback static server. Original IDs, titles,
summaries, priorities, and completion values remain unchanged in the source
fixture.

All changes are in memory; refreshing reads the original sample again.

To inspect this reference, copy the three output files and the fixture's
`tickets.json` into a new temporary directory. Serve only that directory with
an available local static server, bind to `127.0.0.1`, and select a free port.
For example, when Python 3 is already installed:

```sh
python3 -m http.server 8765 --bind 127.0.0.1
```

Record the actual directory, port, server process, and owned browser page.
Complete the [scenario](../skills/frontend-page/references/scenario.md) on
your own setup and stop the owned server afterward. This reference does not
require a new dependency and was not deployed.

## Observed checks

The execution used macOS and the user's existing Chrome. Native Accessibility
and keyboard events operated one owned tab; read-only page queries recorded
actual rows, counts, input values, focus, and geometry after actions. The
initial top-level CSS viewport was 966 by 909 at the existing 90% zoom.

| Check | Action | Observed result | Verdict |
| --- | --- | --- | --- |
| Input and installation | Preview, copy install, doctor, show; compare installed skill, references, and inputs | Installed bytes matched the source; four original tickets retained | Pass for package resources |
| Repository checks | `node --check app.js`; full uncached skill and pluginpkg suites | Syntax check and both relevant package suites exited zero | Pass |
| Initial queue | Load the original local JSON | Four original rows; counts All 4, Open 3, Complete 1; source text and checked state matched | Pass |
| Filters | Tab between filters and activate with Space | Open showed DEMO-101 through DEMO-103; Complete showed DEMO-104 | Pass |
| Completion and no matches | Tab to DEMO-104 in Complete, then Space | Ticket became open; counts 4/4/0; no-match message appeared; focus returned to active Complete filter | Pass |
| Blank title | Navigate to title by Tab, enter three spaces, press Enter | No row added; alert visible; input retained focus and invalid state | Pass |
| Valid addition | Submit the actual native-key input `  rvie` with Enter | Trimmed title `rvie`, ID LOCAL-1, normal priority, open state; counts 5/5/0; input cleared, error cleared, focus retained | Pass |
| Added completion | Shift+Tab to LOCAL-1, then Space | LOCAL-1 became complete; counts 5/4/1; focus stayed at its checkbox | Pass |
| Refresh | Use Chrome's native Reload control | Four source rows and counts 4/3/1 returned; local addition and completion change disappeared | Pass |
| Empty input | Replace only temporary JSON with `[]`, reload | Empty-queue message, zero rows, counts 0/0/0; no read error | Pass |
| Missing input and Retry | Move only temporary JSON aside, reload; restore original bytes; activate Retry with Enter | Failed-read alert appeared; Retry recovered four rows and counts 4/3/1, focusing All | Pass |
| Loading | Hold the local JSON response, reload, then release it | Reading message and busy list observed before four source rows returned | Pass |
| Desktop frame layout | Render the page in same-origin CSS frames at 1280 by 800 and 900 by 700 | No horizontal document overflow; original long title and summary wrapped; controls remained visible in the page | Pass for frame layout |
| Exact top-level sizes | Resize the existing native Chrome window to both requested sizes | Driver exposes no window-bounds primitive; one native resize attempt left the measured viewport unchanged | Not run |

Bulk native text insertion reported success while the DOM value stayed
unchanged. Individual key events did change it. Two assertions initially
expected an intended title or differently capitalized priority rather than
the observed values; neither established a product defect.

Cmd+R delivery also left the page unchanged, so refresh checks used the
observed native Reload button. These failed driver/harness attempts remain
separate evidence.

## Screenshots and review scope

The [1280-wide capture](screenshots/1280-wide.png) and [900-wide
capture](screenshots/900-wide.png) show complete content at those CSS widths
in frames of height 1300. Separate 1280-by-800 and 900-by-700 frame captures
established the requested layout sizes.

The frame wrapper scaled its rendered output to fit the existing browser; it
was temporary and is not part of the reference. Captures were cropped to the
task frame without repainting webpage pixels or publishing browser chrome.

The actual 966-by-909 top-level page was also captured from the document top
and bottom. Native keyboard interactions were verified there. These results
do not establish keyboard acceptance after an exact top-level resize.

The design uses a calm neutral field, restrained green actions, system sans,
thin row separators, and text labels for priority and completion.

A single mechanical detector run returned no findings using a degraded regex
fallback; missing parser modules meant selector and computed-contrast checks
did not run.

An independent agent reviewed the supplied captures, brief, source, and
recorded behavior and returned `ship` for this bounded reference with no
material fixes. It calculated text contrast: primary 12.5:1, supporting
6.01:1, high priority 7.26:1, and white-on-green 7.14:1.

A fresh default agent performed that review because this harness does not
expose the named Impeccable reviewer role. This is independent agent review,
not independent human acceptance.

## Remaining issues

Exact top-level acceptance at both requested sizes, screen-reader behavior,
other browsers, and native Windows/Linux execution remain unrun. Layout
observations in a frame do not settle those checks.

No native Reasonix live-provider, independent human, market publication, or
deployment acceptance is claimed. Package doctor validates installed
resources, not this interface.

## Resources and handoff

The run served only a temporary copy at `http://127.0.0.1:49489/`. Its owned
server was PID 70505. The original input was restored byte-for-byte after
empty, missing, and loading checks. The temporary frame wrapper was removed
and the original generated HTML restored.

The output and cropped evidence were preserved before removing the temporary
workspace.

The owned tab was closed through its observed native close control. Both
preexisting Chrome windows, their other tabs, the browser process, and the
user's frontmost app were retained.

The owned server was terminated and its exec session reported exit 143. No test container was launched.

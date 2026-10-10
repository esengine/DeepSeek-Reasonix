---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Local frontend exercise

This native declarative package bundles a frontend delivery skill, two
references, and a fictional work-queue brief with JSON input. It gives an
author a complete local task to ship with a skill using the existing format.

## Prerequisites

1. Use a Reasonix installation that supports native v2 plugin packages.
2. Configure a model for text and tool calls, and provide browser access for
   acceptance. Package installation does not validate the model or browser.
3. Have an existing local static server available. Python 3 is one optional
   way to serve the exercise; the package adds no runtime dependency.

## Install and run

From the Reasonix repository root, preview and install a copy:

```sh
reasonix plugin install ./examples/frontend-page-kit --dry-run
reasonix plugin install ./examples/frontend-page-kit --yes
reasonix plugin doctor frontend-page-kit
reasonix plugin show frontend-page-kit
```

Use the package root printed by `plugin show` to copy only the exercise inputs:

```sh
package_root='/absolute/path/from/plugin-show'
workspace_root=$(mktemp -d "${TMPDIR:-/tmp}/reasonix-frontend-page.XXXXXX")
cp "$package_root/skills/frontend-page/fixture/brief.md" \
   "$package_root/skills/frontend-page/fixture/tickets.json" "$workspace_root/"
cd "$workspace_root"
```

1. Keep the installed inputs unchanged and record `workspace_root` for cleanup.
   The source checkout is not needed after copy installation. The fixture
   contains task inputs only; the selected model creates the page there.
2. Open a Reasonix session in that temporary workspace. Invoke
   `/frontend-page-kit:frontend-page` with the task below.

```text
Build the local work-queue page described by brief.md using tickets.json.
Produce index.html, app.css, and app.js in this temporary workspace without
adding a dependency. Verify the selected desktop sizes and keyboard workflow
in the available browser. Record screenshots, actual acceptance results,
remaining issues, and resource cleanup using the bundled delivery format.
Do not deploy or publish the page.
```

3. Serve only that workspace using an available local static server. If
   Python 3 is selected, choose a free port and run this from the workspace:

```sh
python3 -m http.server 8765 --bind 127.0.0.1
```

The port above is an example, not a reservation. Record the owned server
process and URL, reuse a suitable existing browser, and create only the page
needed for this exercise. Never stop another task's server to free the port.

## Acceptance and recovery

1. Confirm copy installation retains the skill, both references, and both
   fixture files. Doctor validates the package, not the generated interface.
2. Apply the checks in `references/scenario.md` to the generated page and
   record the results in `references/delivery.md`. Inspect the output files.
3. Preserve `brief.md` and `tickets.json`. For the empty and failed-read
   checks, change only a temporary input copy and restore it afterward.
4. If the model, server, browser, or a check is unavailable, report it as
   not run. A screenshot or a successful install is not functional acceptance.
5. Stop the owned server and confirm it stopped. Close only the exercise's
   pages or sessions; preserve the existing browser and its other tabs.
6. Remove the test package with `reasonix plugin remove frontend-page-kit
   --yes`. Delete only the temporary workspace after preserving wanted output.

This exercise is not a community market submission. Installation and
resource-access tests do not prove that a live model completes the page or
that its design and browser behavior satisfy the brief.

---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# API notes kit

A native declarative package showing how to ship a source-linked documentation
skill with references, original inputs, and a local checker. The fictional
Notes API fixture and all other content inherit this repository's MIT license.
This is an author template, not a reviewed community-market listing.

The skill writes a Markdown guide from local OpenAPI JSON. It leaves review
status explicit and separates declared behavior from unknowns.

Python 3 is
needed only for the bundled fixture checker; it uses the standard library.
No API server, browser, account, credential, network call, or additional package
is needed for the local scenario. A model must support reading local files and
running the checker; no vision capability is required.

## Preview and install

From the repository root, using a built Reasonix CLI:

```sh
reasonix plugin install ./examples/api-notes-kit --dry-run
reasonix plugin install ./examples/api-notes-kit --yes
reasonix plugin doctor api-notes-kit
reasonix plugin show api-notes-kit
```

Review the preview before installing. The approved install copies the package,
including the reference files and fixtures. In Studio, the corresponding
local-folder install uses the same preview/confirmation path. Use the package
root reported by `plugin show`, not the original checkout, for this exercise.

## Run the local scenario

Set `package_root` to the installed package directory printed by `plugin show`:

```sh
package_root='/absolute/path/from/plugin-show'
python3 "$package_root/fixture/check_refs.py" "$package_root/fixture/openapi.json"
python3 "$package_root/fixture/check_refs.py" "$package_root/fixture/broken-reference.json"
python3 -B -m unittest discover -s "$package_root/fixture"
```

The first command must exit 0. The second must exit 1 and locate the unresolved
response reference. Neither command validates OpenAPI against its schema or
calls a server.

Do not count the expected nonzero result as a successful source.
The unit tests check pointer escapes, array-index rules, missing/external
targets, and recursive references; they also run without network access.

Create a temporary output directory and select it as the task's write scope.
Invoke `/api-notes-kit:api-notes` with the absolute path to the installed
`fixture/openapi.json` and ask for `api-notes.md` in the output directory.

Follow `skills/api-notes/references/scenario.md`; it defines
both the minimum guide and the failure behavior. Check each generated claim
against the original JSON Pointer and leave its human-review status pending.
Record observed commands and unresolved questions with the draft.

Successful package installation and checker results do not establish that a
model produced an accurate guide. A model task and human factual review are
separate acceptance steps.

Do not publish or call the API during this exercise.
The checker supports direct local JSON Pointer references only; external or
URI-encoded references need separately reviewed tooling or source material.

## Compare the fixture draft

1. Open the installed package's [worked guide](fixture/example-guide.md) after
   drafting. It is bound to the bundled `openapi.json` byte hash and shows the
   required operations, models, source citations, unknowns and review record.
2. Resolve each citation in your own guide and compare the claim with that
   source object. Check missing declarations as well as stated values; optional
   fields, absent error responses and unspecified guarantees need distinct
   treatment.
3. Keep human-review status pending until a person reviews the claims. The
   worked draft and its observed local checker results do not certify another
   model output or a running service. For a different source, read its entire
   document and create a new guide tied to that source.

## Disable and clean up

```sh
reasonix plugin disable api-notes-kit
reasonix plugin enable api-notes-kit
reasonix plugin remove api-notes-kit --yes
```

Disable prevents the installed package's skills from being offered on the next
eligible projection. Re-enable restores discovery.

Remove deletes the copied
installation; it does not delete your source checkout or generated guide.
After retaining any output you need, remove only the temporary directory you
created for the exercise. Keep unrelated installed packages and user files.

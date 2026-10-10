---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Local scenario

The fixture is original sample content distributed under the repository's
MIT license. It represents a fictional Notes API, not a running service.

## Input

Use the installed package's `fixture/openapi.json`. Select a temporary output
directory and ask: "Use api-notes to write a source-linked guide for this local
OpenAPI document. Do not call a service. Leave factual claims pending human
review and identify behavior the source does not specify."

The minimum guide must cover GET and POST `/notes`, the `X-Api-Key` scheme,
GET's offset/limit defaults and bounds, the required POST title, the optional
body, the Note and Notes schemas, and the declared 200/201 responses.

Cite the
relevant operation, parameter, security-scheme, and schema pointers. Do not
claim ordering, total counts, role permissions, rate limits, or error status
codes: the source does not declare them.

## Failure input

`fixture/broken-reference.json` contains a response reference to a missing
schema. Its checker must exit nonzero.

Report the unresolved source pointer
and request corrected material. Do not generate a complete response model or
silently substitute a similarly named schema. This is a valid JSON document,
so a JSON syntax check alone cannot detect this failure.

## Acceptance

Run the checker on both inputs and record the actual exit codes. For a generated
guide, open the source and verify every claim and citation manually.

Confirm
that both operations and the stated unknowns are present and that the review
record does not claim human approval. Output is a local Markdown draft only.
Checker results establish source-reference integrity, not model task quality.

The installed `fixture/example-guide.md` provides a worked draft for this
exact fixture. Compare coverage, citations and stated unknowns after writing
your own guide. Its source byte hash must match the input being reviewed;
another source needs its own claims and review record.

Delete only the temporary output directory selected for this exercise. Do not
edit the installed fixture to make the failure input pass.

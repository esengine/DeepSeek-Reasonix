---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-06
---

# Fixture Notes API guide

## Source and scope

1. This local draft describes the fictional **Fixture Notes API**, version
   **1.0.0**, declared as OpenAPI **3.0.3**. Sources:
   `openapi.json#/info` and `openapi.json#/openapi`.
2. The original fixture inherits this repository's MIT license. The input's
   byte SHA-256 is `e4b0ff88fcb38aa979ee1610038e88603e377a4faec5566aa4aa1b2d49602d6e`.
   All citations below refer to the sibling `openapi.json`, not a live service.
3. No live API call or OpenAPI schema validation is recorded. A local reference
   check verifies reference targets only. All factual claims remain pending
   human review.

## Authentication

| Declaration | Source |
| --- | --- |
| Document-level security uses `apiKeyAuth`. Neither operation declares a security override. | `openapi.json#/security`, `openapi.json#/paths/~1notes` |
| `apiKeyAuth` is an API key in the `X-Api-Key` header. | `openapi.json#/components/securitySchemes/apiKeyAuth` |

The document does not explain credential provisioning or roles. Its API-key
declaration does not establish that authentication has been tested against a
running service.

## Operations

### GET /notes

| Item | Declaration | Source |
| --- | --- | --- |
| Purpose | List notes. | `openapi.json#/paths/~1notes/get/summary` |
| `offset` | Optional query integer; default `0`, minimum `0`. | `openapi.json#/paths/~1notes/get/parameters/0` |
| `limit` | Optional query integer; default `10`, minimum `1`, maximum `50`. | `openapi.json#/paths/~1notes/get/parameters/1` |
| Success | `200`, described as a page of notes. Its `application/json` schema references `Notes`. | `openapi.json#/paths/~1notes/get/responses/200` |

`Notes` is an array of `Note` objects; follow both model definitions below.
The GET response declares no total-count field or ordering guarantee. The
only response entry is `200`; error status codes and error bodies are absent
from `openapi.json#/paths/~1notes/get/responses`.

### POST /notes

| Item | Declaration | Source |
| --- | --- | --- |
| Purpose | Create a note. | `openapi.json#/paths/~1notes/post/summary` |
| Body | Required request body with an `application/json` object schema. | `openapi.json#/paths/~1notes/post/requestBody` |
| Required field | `title` is required and has type `string`. | `openapi.json#/paths/~1notes/post/requestBody/content/application~1json/schema` |
| Optional field | `body` has type `string` and is absent from the required list. | `openapi.json#/paths/~1notes/post/requestBody/content/application~1json/schema` |
| Success | `201`, described as the created note. Its `application/json` schema references `Note`. | `openapi.json#/paths/~1notes/post/responses/201` |

No minimum string length is declared for the request fields. The only response
entry is `201`; error status codes and error bodies are absent from
`openapi.json#/paths/~1notes/post/responses`.

## Models

| Model | Declaration | Source |
| --- | --- | --- |
| `Note` | Object with required `id` and `title`; both are strings. `body` is a string outside the required list. | `openapi.json#/components/schemas/Note` |
| `Notes` | Array whose items reference `Note`. No ordering, total count or pagination metadata is declared. | `openapi.json#/components/schemas/Notes` |

An optional property may be absent. That does not establish support for an
explicit JSON `null`: these schemas do not declare `nullable`. No enumerated
values or string-length bounds are declared for the model fields.

## Unknowns and limitations

1. The local references resolve for this fixture. The checker does not prove
   that the entire OpenAPI document is valid or that its prose is accurate.
2. Rate limits, role permissions and credential issuance are unspecified.
   No `401`, `403`, `404` or other error response is declared; callers need
   additional reviewed material before documenting those behaviors.
3. Note ordering, total counts, pagination consistency under concurrent
   changes and live persistence guarantees are unspecified.
4. This draft is specific to the byte hash above. A different source must be
   read and checked independently; its declarations must not be filled in by
   copying this guide.

## Review record

| Claim | Source pointer | Review status |
| --- | --- | --- |
| API title/version and OpenAPI version. | `openapi.json#/info`, `openapi.json#/openapi` | pending human review |
| Global API-key security without operation overrides. | `openapi.json#/security`, `openapi.json#/paths/~1notes` | pending human review |
| Header name and API-key location. | `openapi.json#/components/securitySchemes/apiKeyAuth` | pending human review |
| GET's declared purpose and response. | `openapi.json#/paths/~1notes/get` | pending human review |
| Optional offset, default and lower bound. | `openapi.json#/paths/~1notes/get/parameters/0` | pending human review |
| Optional limit, default and both bounds. | `openapi.json#/paths/~1notes/get/parameters/1` | pending human review |
| POST's declared purpose, body and response. | `openapi.json#/paths/~1notes/post` | pending human review |
| POST title/body types and required list. | `openapi.json#/paths/~1notes/post/requestBody/content/application~1json/schema` | pending human review |
| Note object, field types and required list. | `openapi.json#/components/schemas/Note` | pending human review |
| Notes array and item reference. | `openapi.json#/components/schemas/Notes` | pending human review |
| Missing error declarations, bounds, enumerations and guarantees. | `openapi.json#/paths`, `openapi.json#/components` | pending human review |

The following observed checks are separate from the claim-review table:

| Command | Observed result | What it establishes |
| --- | --- | --- |
| `python3 -B check_refs.py openapi.json` | Exit `0`. | Local reference targets resolve. |
| `python3 -B check_refs.py broken-reference.json` | Exit `1`. | The failure input's response reference is unresolved. |
| `python3 -B -m unittest discover -s .` | Exit `0`. | The bundled checker tests pass. |

These commands run from the installed package's `fixture/` directory. They do
not establish human approval, live API behavior or the factual accuracy of a
different generated guide.

## Failure input

1. The separate failure input reports this source location:
   `broken-reference.json#/paths/~1notes/get/responses/200/content/application~1json/schema/$ref`.
2. Its target is `#/components/schemas/MissingNote`, which is absent. A complete
   response model cannot be documented from that input. Request the missing or
   corrected local schema; do not substitute `Note` or edit the installed
   fixture to make the checker pass.

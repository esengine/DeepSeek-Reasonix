---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Guide format

Use these sections in `api-notes.md`:

1. **Source and scope.** Source path, declared API title/version, and whether
   the input is a fixture or a real service specification. State that no live
   API call was made unless a separate authorized check actually ran.
2. **Authentication.** Declared scheme and header/query location; distinguish
   document-level security from operation-level overrides. Say which behavior
   is not specified, including who issues credentials and any role policy.
3. **Operations.** One subsection per method/path. Document declared inputs,
   defaults, bounds, required fields, success responses, and declared errors.
   Inline source citations use `source.json#/paths/~1notes/get`, for example.
   Escape `/` as `~1` and `~` as `~0` in a JSON Pointer token.
4. **Models.** Fields, types, required lists, and declared enumerations, with
   pointers to the schema. A `$ref` points to another source object: read it
   before describing it. Preserve the difference between absent and optional.
5. **Unknowns and limitations.** Unresolved/external references, missing error
   responses, unspecified ordering, pagination consistency, rate limits, or
   credential provisioning. Do not fill the gaps with likely behavior.
6. **Review record.** A table with Claim, Source pointer, and Review status.
   Use `pending human review` until a person has actually reviewed that claim.
   Add observed command results separately, including scope and limitations.

A useful citation identifies the object supporting the claim, not merely the
root of the document. A valid pointer alone does not prove that the claim is
true. A JSON syntax check, reference check, schema check, and live API test are
four different checks; name only the checks actually run.

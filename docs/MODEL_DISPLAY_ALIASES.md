---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-12
---

# Model display aliases

In Studio's model service settings, each selected model has an optional display
alias beneath its full model ID. Save the connection to apply it. Leave the alias
empty to show the model ID again. Revert discards unsaved changes with the rest of
the connection form.

The chat model picker and its active-model button show the alias. The picker keeps
the full model ID below it and searches both names. Duplicate aliases are allowed:
the provider and raw model ID still identify the selection.

Hovering the active-model button shows the connection's display name and model
alias, using the picker's account naming. Saved settings refresh both names
without changing the selected model.

- Without a model alias, the tooltip shows the model ID.
- If the active model is absent from the catalog, it shows the full reference.

Aliases are scoped to one provider entry and one exact model ID, including case
and any slash in the model ID. They do not change provider requests, defaults,
role assignments, saved conversation references, or model availability. Editing
only aliases does not rebuild or interrupt a running conversation.

The user configuration stores them in the provider's `model_aliases` map:

```toml
[[providers]]
name = "local"
kind = "openai"
base_url = "http://localhost:11434/v1"
models = ["vendor/long-model-id"]
model_aliases = { "vendor/long-model-id" = "Short name" }
```

Aliases use the display-name limit of 64 Unicode characters and cannot contain
control characters. Unselecting a model retains its stored alias for later use;
clearing a selected model's alias removes it. Clients that omit `modelAliases`
when editing a provider preserve existing aliases.

Resolver-backed remote panes use their resolver's catalog. This machine's local
aliases are never overlaid onto that catalog. Removing fetched models is a
separate part of [#11774](https://github.com/esengine/DeepSeek-Reasonix/issues/11774).

# Model capability metadata

Reasonix resolves input capabilities per model through the provider adapter.
Adapters return `inputModalities` for the exact model, following the
`deepseek-harness` model contract:

- `text` means text input is accepted.
- `image` means native image input is accepted.
- `text + image` enables native multimodal requests without a `VisionModels`
  setting.

OpenAI-compatible `/models` responses may use the canonical
`input_modalities` field. Reasonix also accepts `modalities.input`,
`capabilities.input_modalities`, `capabilities.vision`, `supports_vision`, and
`vision` as compatibility aliases. Missing, malformed or conflicting declarations
remain **unknown** (`nil` internally; `[]` in the Desktop view). A valid text-only
declaration is **unsupported**; an image declaration is **supported**. Standard
fields take priority over aliases, including when the standard value is invalid.
Duplicate IDs merge independently of response order: unknown observations do not
erase valid facts, and contradictory facts remain unknown. Model names are never
used to guess image support.

Dynamic metadata is stored in the disposable
`model-capabilities-v2.json` cache under the Reasonix cache directory. It is
not written to `config.toml`. Existing `vision` and `vision_models` entries
remain readable for backwards compatibility and take precedence over dynamic
metadata. V2 neither reads nor changes V1: the old cache cannot distinguish
missing metadata from a negative declaration. Custom models relying only on V1
positive metadata need one model-list refresh or a manual override. Cache entries
expire after 24 hours; failed requests do not replace successful entries, while a
successful ID-only response records unknown. Route and credential changes isolate
the cache; late requests cannot replace newer successful discoveries.

Built-in adapters also ship verified local catalogs for all untouched curated
provider presets. The catalogs cover the official OpenCode Go routes, the
DeepSeek vision SKU, ModelScope Qwen3.5 SKUs, and the remaining preset model
lists. They work without a model-list request; a custom endpoint, edited preset,
or model not in a local catalog stays unknown unless another valid source applies.

## Set image input for a relay model

1. In Settings → Models, add or edit the provider and fetch its model list.
2. Select the model. If it shows “Image capability unknown”, confirm support with
   the service provider and choose **Image input → On**.
3. Save, then send an image after the runtime rebuild succeeds. Refreshing the
   list and restarting the app preserve the choice.

Both the provider editor and refreshed model picker offer **Auto / On / Off**.
On is your declaration of support, not a paid client probe. Off prevents native
image input even for catalogued visual models. Auto removes only the model's
`vision` override; context/output/reasoning settings remain intact. Auto can say
“Using legacy configuration” when an older `vision`/`vision_models` value applies.

```toml
[providers.model_overrides.example-model]
vision = true # false disables; remove this field for Auto
```

Final priority is official protocol restriction → per-model override → existing
automatic chain (curated preset, legacy config, exact local catalog, valid online
cache) → unknown. Official DeepSeek text models remain blocked; its visual model
can be explicitly disabled. Capability changes preserve catalog context, output,
API and reasoning metadata. Discovery never saves UI-provided capabilities as facts.

Idle active sessions rebuild after saving. Other open sessions check before the
next turn; an in-flight request retains its frozen capability and payload. Failed
or deferred rebuilds must finish before the setting is effective. The composer
reads the running Controller snapshot. No failed request or historical image is
automatically resent, and sending a message never fetches `/models`.

System prompts, tool schemas and text-only serialization stay unchanged. Changing
image capability can change the projection of image-bearing history, so cache hits
for such conversations are not guaranteed across a rebuild.

The broader provider/model catalog is sourced from the MIT-licensed
`github.com/sky-valley/pi/ai` Go port of Pi. Reasonix uses its embedded model
data only (`GetModels`/`Model.Input` and related facts), not its Agent or
Provider runtime. The dependency is pinned in `go.mod`; catalog updates must
be reviewed as data and license changes.

Dependabot opens a dedicated weekly `sky-valley/pi` update PR instead of
bundling it with unrelated Go upgrades. The catalog contract tests in
`internal/provider/opencode_go_test.go` fail when important model capabilities
drift, so an update is merged only after its provider/API/endpoint diff is
reviewed.

Text-only and unknown models use the existing `Agent.VisionModel`, OCR, and MCP
vision fallback paths. Raw image payloads are never sent to those models.

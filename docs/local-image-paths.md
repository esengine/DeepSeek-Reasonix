# Read local images by path

Give the agent an accessible local image path and ask it to inspect the image. The built-in `view_image` tool reads PNG, JPEG, GIF, and WebP files and returns structured image content. Native vision takes precedence. Text-only models use the configured image-understanding model to produce a summary; without one, the result explicitly says the image was not understood. A path alone does not upload an image.

Relative paths resolve against the task workspace. Session external-folder aliases are supported. Existing sensitive-file and forbidden-directory rules apply, including symlink targets. Files must be regular files, no larger than 3 MiB, with at most 40 million pixels. Image format is detected from the content rather than the filename.

OpenAI Chat and supported Anthropic adapters already forward tool images. Responses now appends image content after the complete group of tool results, preserving call/result order. Text-only models receive no image payload. The official DeepSeek vision SKU receives tool images through appended user image content (top-level user image blocks for Anthropic). Ordinary Flash/Pro can use the configured image-understanding fallback instead of receiving image bytes.

Adding `view_image` changes the stable tool schema prefix once after upgrading and can cause an initial prompt-cache miss. Tool definitions do not vary per turn. No stored conversation format changes are required.

All structured tool images, including direct and on-demand MCP images, share the same service. `vision_model=auto` selects only within the current provider. Summaries identify their source and remain untrusted context. Content-based caching is session-local; mutable URLs are not reused without content verification. Summary failures preserve the original tool text and do not repeat the original action.

## Validation

Deterministic coverage includes cancellation, concurrent cache reuse, restored tool-message summaries, subagent isolation, on-demand MCP, and preserving execution results when image understanding fails. Run `go test -race ./internal/imageinput ./internal/agent ./internal/control` for the owning packages.

The opt-in live probe uses generated images with random codes and colored rectangles. It reads configured official DeepSeek credentials without printing them and tests three native protocols plus explicit/automatic summary-service routing with changed images:

```sh
REASONIX_LIVE_TOOL_IMAGES=1 go test -tags live ./internal/imageinput -run TestLiveToolImages -v -count=1
```

The live `auto` case injects a same-provider selector to exercise the service route; actual configured catalog selection is owned by Boot. Live probes incur API usage. A successful request alone is insufficient: image-derived content must pass the assertions. OCR can still misread ambiguous characters, and summaries are not a substitute for pixel-exact visual access.

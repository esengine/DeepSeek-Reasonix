# Read local images by path

Give the agent an accessible local image path and ask it to inspect the image. The built-in `view_image` tool reads PNG, JPEG, GIF, and WebP files and returns structured image content. Native vision takes precedence. Text-only models use the configured image-understanding model to produce a summary; without one, the result explicitly says the image was not understood. A path alone does not upload an image.

Relative paths resolve against the task workspace. Session external-folder aliases are supported. Existing sensitive-file and forbidden-directory rules apply, including symlink targets. Files must be regular files, no larger than 3 MiB, with at most 40 million pixels. Image format is detected from the content rather than the filename.

## Composer attachments

Pasted and dropped files are first saved under `.reasonix/attachments` in the workspace of the tab that started the operation, then admitted into the session content store as immutable originals. Saving, previewing, and native vision all retain that tab and workspace identity across asynchronous work; switching tabs does not redirect a result into the newly active draft. Target-scoped operations negotiate `attachments-v2` before browser file reads or hashing; the legacy tab API retains its `attachments-v1` capability name. Neither path trusts client-supplied digests. Legacy `@.reasonix/attachments/...` references remain readable without rewriting history. History cards for admitted images carry digest-only attachments; the desktop loads original bytes through `ReadSessionAttachmentForTab` after the session content graph authorizes that digest. Remote Serve advertises neither local staging capability.

Explicit image attachments are read, fully decoded, and persisted before a turn is accepted or the draft is cleared. A missing, unreadable, unsafe, unsupported, damaged, oversized, canceled, or concurrently changed image rejects the complete turn before model or tool execution. The composer keeps its text and attachments so the user can restore the file or attach it again and retry. Non-image references keep their existing behavior. After admission, later edits or deletion of the original workspace file do not change the persisted original.

New messages store ordered `image_inputs` (content-addressed attachment refs, external URLs, or Files IDs). The legacy `images []string` field remains the read path for old data URLs, HTTP URLs, and Files IDs. A message must not carry both fields. Session `StorageRevision` is 3 and the inbox schema is 3: this build reads older data; older builds refuse the new session format and pause newer inboxes read-only.

OpenAI Chat and supported Anthropic adapters already forward tool images. Responses now appends image content after the complete group of tool results, preserving call/result order. Text-only models receive no image payload. The official DeepSeek vision SKU receives tool images through appended user image content (top-level user image blocks for Anthropic). Ordinary Flash/Pro can use the configured image-understanding fallback instead of receiving image bytes. Files uploads happen while preparing the actual model request and use that request's context; they are not written back into history.

Adding `view_image` changes the stable tool schema prefix once after upgrading and can cause an initial prompt-cache miss. Tool definitions do not vary per turn. Old history images keep their previous provider-visible bytes. New attachments use a deterministic request variant (longest side 1568px, policy version 1), which can miss prompt cache on the first turn after upgrade.

All structured tool images, including direct and on-demand MCP images, share the same service. `vision_model=auto` selects only within the current provider. Summaries identify their source and remain untrusted context. Content-based caching is session-local; mutable URLs are not reused without content verification. Summary failures preserve the original tool text and do not repeat the original action.

## Compatibility

| Data | Old writer | This build | Previous build |
| --- | --- | --- | --- |
| Old `images []string` (data URL / HTTP / Files ID) | Written | Read as-is; not rewritten | Read as-is |
| New `image_inputs` attachment refs | Not written | Written on new messages after StorageRevision 3 | Session refused (`ErrUnsupportedVersion`) |
| Workspace `.reasonix/attachments/...` files | Written | Still a composer/CLI entry; admitted objects live in `.content-v1` | Read as files |
| Inbox schema 2 | Written | Migrated to 3 on writer open | Read/write |
| Inbox schema 3 with `imageInputs` | Not written | Written | Read-only pause |

## Validation

Deterministic coverage includes cancellation, concurrent cache reuse, restored tool-message summaries, subagent isolation, on-demand MCP, and preserving execution results when image understanding fails. Run `go test ./internal/attachment ./internal/imageinput ./internal/agent ./internal/control ./internal/session ./internal/sessioninbox ./internal/tool/builtin` for the owning packages. Export/import walks sibling `.inbox` blobs and refuses missing content-addressed objects. Previous readers reject StorageRevision 3 and pause inbox schema 3 read-only.

Attachment regressions additionally cover a process working directory different from the workspace, same-named images in separate workspaces, tab and runtime replacement during I/O, and atomic rejection for missing, damaged, escaping, symlinked, oversized, or partially invalid image sets. Deterministic checks exercise both workspace-write and full-access permission profiles with the same image; neither automatic image input nor `view_image` requires a process-directory change.

The opt-in live probe uses generated images with random codes and colored rectangles. It reads configured official DeepSeek credentials without printing them and tests three native protocols plus explicit/automatic summary-service routing with changed images:

```sh
REASONIX_LIVE_TOOL_IMAGES=1 go test -tags live ./internal/imageinput -run TestLiveToolImages -v -count=1
```

The live `auto` case injects a same-provider selector to exercise the service route; actual configured catalog selection is owned by Boot. Live probes incur API usage. A successful request alone is insufficient: image-derived content must pass the assertions. OCR can still misread ambiguous characters, and summaries are not a substitute for pixel-exact visual access.

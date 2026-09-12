# Present tool and file deliverables

`present` records files that an agent has finished and wants the user to open or keep. It complements the automatic per-turn file-change index: a file can be presented even when it was created by Bash, Python, or another tool that does not emit a structured file edit.

```json
{
  "files": [
    { "path": "minesweeper.html", "description": "Interactive Minesweeper game" },
    { "path": "README.md", "description": "Instructions" }
  ]
}
```

The call accepts one to eight existing regular files. Relative paths resolve from the session workspace; absolute and external-reference paths must already be readable by that session. Validation is atomic: a missing, forbidden, directory, or final symlink entry fails the entire call. The tool reads metadata only and does not execute, upload, copy, or open a file. A successful declaration is stored as host metadata with `version: 1` alongside the trusted tool result and survives history replay and conversation forks. An unknown metadata version remains preserved but is not exposed as a trusted file entry.

The desktop shows deliverable cards after the final answer and before the turn actions. Clicking a card opens the read-only document workspace. Its menu can open a browser-capable file in an isolated built-in browser, reveal the file tree, show source, copy the source host's absolute path, use native file actions, or save a local copy. Every open, copy, and native action revalidates the trusted declaration, current file state, and current read-deny policy. Authorized deliverables outside the workspace appear in a temporary **External files** scope; they are not added to the project and do not widen directory permissions. Remote cards never pass a remote path to local native-open APIs; they can copy the remote coordinate, be located in the remote tree, or be streamed to a user-selected local destination.

Built-in previews cover HTML, Markdown, text and source code, JSON, CSV/TSV, images, PDF, audio, and video. UTF-8 text beyond the initial 2 MiB is loaded in version-fenced pages; a file change stops pagination and requires a reload so two revisions are never joined. Office documents, archives, and unsupported binaries retain file, reveal, copy, save, and native-open actions without an in-app conversion engine. Up to five document tabs are retained, with the least recently used inactive tab released when the limit is exceeded. Changed files keep the current preview until the user reloads it.

HTML previews use a loopback-only, revocable capability URL and a sandboxed context without the application bridge, Node.js, or shared browser login state. Direct local CSS, classic scripts, images, and media are validated and bound before the preview is published. Each dependency is limited to 4 MiB, each bundle to 64 dependencies and 32 MiB. The preview cannot browse directories or fetch arbitrary local files.

The structured presentation metadata is host-only and is removed from provider requests. The tool schema and instruction are fixed and registered in stable order. Adding the tool changes the request prefix once after upgrade and may temporarily reduce prompt-cache hits; opening previews and switching permission modes do not rewrite that prefix.

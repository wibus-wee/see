# Semantic Tokens, Document Links, and Code Actions — Feature Candidates

Three LSP capabilities that extend See's code reading: precise type-aware highlighting,
clickable file/URL references, and quick fixes on diagnostics.

## How to read each candidate

Each section is self-contained: scope, UX contract, implementation map with the files
that own each seam, test entry points, and gotchas from prior worker experience.

---

## S1 — Semantic Tokens

Replace regex-based syntax highlighting with LSP `textDocument/semanticTokens/full`
(and optionally `textDocument/semanticTokens/range` for viewports) for Go files.
The regex tokenizer stays as fallback for unsupported languages and files without
an LSP client.

### UX contract

- Go files opened with an LSP-backed editor show semantic coloring: parameters vs.
  fields vs. types vs. constants vs. package names each get a distinct style.
- Scrolling triggers `semanticTokens/range` for the visible viewport; a full-file
  request fires once on open and after edits settle.
- If the server doesn't advertise `semanticTokensProvider`, the editor falls back
  to the existing regex tokenizer — the reader should not notice a difference.
- Semantic highlighting overrides the regex tokenizer for supported files. There
  is no user-facing toggle initially (it's a strict improvement), but a hidden
  config key or debug flag could help compare during development.

### Implementation map

**LSP protocol** (`internal/lsp/protocol.go`):
- Add `SemanticTokensProvider` `json.RawMessage` to `serverCapabilities`.
- The provider is an object with `legend` (`{tokenTypes: string[], tokenModifiers: string[]}`),
  `full` (`bool | {delta: bool}`), and `range` (`bool | {}`). Parse the legend —
  it maps the numeric token indices the server returns to type names.
- `hasSemanticTokens()` — true when the provider's `full` or `range` is truthy.

**LSP client** (`internal/lsp/client.go`):
- `SemanticTokens(ctx, uri string, r *Range) (*SemanticTokensData, error)` —
  call `textDocument/semanticTokens/full` when `r == nil`,
  `textDocument/semanticTokens/range` when provided.
- `SemanticTokensData` holds `Data []uint32` — the delta-encoded token stream.
  Decoding: every 5 `uint32`s are `(deltaLine, deltaChar, length, tokenType, tokenModifiers)`.
- `HasSemanticTokens()` accessor.
- Store the server's `legend` on `Client` after initialize — token type indices
  are server-specific, so `Client` must expose `SemanticTokenTypes() []string`
  and `SemanticTokenModifiers() []string`.
- In `initialize` capabilities: `"semanticTokens": {"dynamicRegistration": false,
  "requests": {"range": true, "full": true}, "tokenTypes": [...standard LSP types...],
  "tokenModifiers": [...standard LSP modifiers...], "formats": ["relative"]}`.

**App layer** (`internal/app/semantic_tokens.go` — new):
- `fetchSemanticTokens(d *doc)` — mirror `fetchInlayHints`: goroutine, sync buffer,
  call, post result. Key differences:
  - Requests `range` covering the visible lines (like inlay hints) rather than full —
    the file can be huge and the reader only sees ~80 lines.
  - On first open, also fire `full` to seed the whole file.
  - Delta-decode the `[]uint32` stream into `(offset, length, tokenType, modifiers)`
    pairs using the negotiated `PositionEncoding`.
  - Map LSP token type names to `editor.TokenKind` values — e.g. `"type"` → `TokenType`,
    `"parameter"` → `TokenVariable` (or a new kind), `"keyword"` → `TokenKeyword`.
  - The `editor.TokenKind` set may need extending — check what kinds mygo supports.
    If it doesn't cover semantic categories (e.g. `namespace`, `property`, `interface`),
    add new kinds or map to the closest existing one.
- Cache per `(first, last, version)` like `inlayKey`.

**Editor integration** (`internal/app/editor_area.go`):
- After `st.SetInlays(...)`, apply semantic token overlays:
  `st.SetSemanticTokens(a.editorSemanticTokens(d, st))` — or whatever mechanism
  the editor exposes for token-level coloring beyond the base tokenizer.
- If the editor only supports a single token stream (the tokenizer's), the semantic
  layer may need to replace rather than overlay — check mygo's `editor.State`
  for how tokens and styling work together.

**Theme** (`internal/app/theme.go`):
- Map each new/mapped `TokenKind` to a theme color. The existing themes should
  already have enough colors; just assign them.

### Test entry points

- `internal/lsp/lsp_test.go` — `fakeServer` add `textDocument/semanticTokens/full`
  and `/range` cases returning delta-encoded data; test `HasSemanticTokens`,
  `SemanticTokens()`, legend parsing, and decoding.
- `internal/app/harness_test.go` — `fakeClient` add `semanticTokens` field and
  `SemanticTokens`/`HasSemanticTokens` methods.
- `internal/app/semantic_tokens_test.go` — test token decoding, type mapping,
  viewport range computation, fallback when server lacks the capability.

### Gotchas

- **Delta encoding**: LSP uses relative positions — `deltaLine` is relative to the
  previous token, `deltaChar` is relative to the previous token's start *only when
  `deltaLine == 0`*, otherwise it's absolute within the line.
- **Position encoding**: tokens are in the negotiated encoding (UTF-8 or UTF-16).
  Convert to buffer offsets the same way `toInlays` does.
- **Token type indices are server-specific**: the `legend` returned during
  `initialize` defines the ordering. Do not hardcode indices.
- **mygo editor.TokenKind** may not have enough categories. Check
  `editor.TokenKind` constants; if limited, add new kinds in mygo or use
  a separate overlay mechanism.
- **Performance**: `semanticTokens/range` on every scroll could be expensive.
  Debounce like inlay hints (the existing `inlayKey`/`fetchInlayHints` pattern).
- **Regex tokenizer still runs**: the tokenizer is synchronous per line and cheap.
  Semantic tokens are an overlay or replacement — make sure the visual result
  doesn't fight between two systems.

---

## S2 — Document Links

`textDocument/documentLink` — make import paths, URLs in comments, and file
references clickable. Clicking opens the target (a file in the project, or a URL
in the browser).

### UX contract

- Underlined, cursor-changes-to-pointer text on import paths (`import "fmt"`),
  URLs in comments (`// https://pkg.go.dev/fmt`), and relative file references
  (`//go:embed assets/style.css`).
- Click (or Cmd+click) opens the target:
  - Relative file path → open in a tab
  - `import "pkg"` → resolve to the package's files (Go: gopls may return file://
    URIs for standard library, or the module cache for deps)
  - `https://...` → open in browser
- Links are invisible when the server doesn't support document links — the text
  looks normal.
- Links refresh on edit (the server re-evaluates the whole document).

### Implementation map

**LSP protocol** (`internal/lsp/protocol.go`):
- Add `DocumentLinkProvider` `json.RawMessage` to `serverCapabilities`.
- The provider is `{resolveProvider: bool} | bool`. Parse `resolveProvider`.
- `hasDocumentLinks()` — true when truthy.
- `DocumentLink` struct: `Range`, `Target string`, `Tooltip string`,
  `Data json.RawMessage` (for lazy resolution).

**LSP client** (`internal/lsp/client.go`):
- `DocumentLinks(ctx, uri string) ([]DocumentLink, error)` — call
  `textDocument/documentLink`.
- `DocumentLinkResolve(ctx, link *DocumentLink) (*DocumentLink, error)` — call
  `documentLink/resolve` when `resolveProvider` is true and `Target` is empty.
- `HasDocumentLinks()` accessor.
- In `initialize` capabilities: `"documentLink": {"dynamicRegistration": false,
  "tooltipSupport": true}`.

**App layer** (`internal/app/document_links.go` — new):
- `fetchDocumentLinks(d *doc)` — like `fetchInlayHints`, but full-document
  (not range-based). Debounce on buffer version.
- For links with empty `Target` and `resolveProvider`, call `DocumentLinkResolve`
  lazily on click (or eagerly after the list returns, for < 50 links).
- Cache on `doc`: `d.documentLinks []DocumentLink`, `d.documentLinkVersion uint64`.

**Editor integration** (`internal/app/editor_area.go`):
- Store links per doc. The rendering can use:
  - `editor.Inlay` for visual marks? No — links need hit-testing and tooltips.
  - Better: `editor.State` may have a `SetLinks` or similar mechanism. Check.
  - If not, use `editor.LineBackgrounds` or a custom draw pass for underline.
- Click handling: intercept clicks in the editor area, check if the click falls
  inside a link range, then open the target.
- The link underline style: a subtle underline on the link text range. The editor
  may need a new API for this — `editor.Span` or a `SetLinkRanges` method.

**Navigation** (`internal/app/navigation.go` or a new `links.go`):
- `openLink(link DocumentLink)` — dispatch on `Target` scheme:
  - `file://` → `uri.ToPath` → `a.openFile` (existing tab open)
  - `http://`/`https://` → `openURL` (use `open` on macOS, `xdg-open` on Linux,
    `cmd /c start` on Windows — check if mygo has a `ui.OpenURL` or use
    `os/exec`)
  - Empty/relative → treat as workspace-relative path

### Test entry points

- `internal/lsp/lsp_test.go` — `fakeServer` add `textDocument/documentLink` case;
  test `HasDocumentLinks`, `DocumentLinks`, `DocumentLinkResolve`.
- `internal/app/harness_test.go` — `fakeClient` add `documentLinks` field.
- `internal/app/document_links_test.go` — test link parsing, URI→path conversion,
  click dispatch, and the no-server fallback.

### Gotchas

- **Link ranges are per-line**: a link spans characters on one line. The editor
  must support hit-testing on arbitrary ranges.
- **gopls document links** — gopls implements `textDocument/documentLink` for
  `import` strings and `//go:embed` directives. `resolveProvider` is false —
  targets come back already resolved.
- **Click interception**: the editor's click handling must not break when a link
  overlaps a selection or the cursor. Check how `editor` handles pointer events.
- **External links**: opening URLs needs a platform-aware `openURL`. Check if
  mygo already has this (it might — it's a desktop framework).
- **Range encoding**: link ranges are in the negotiated position encoding.
  Convert to buffer offsets for hit-testing.

---

## S3 — Code Actions

`textDocument/codeAction` — quick fixes and refactorings surfaced on diagnostics.
The reader sees an error, clicks the lightbulb (or presses Cmd+.), and gets a
menu of fixes the server offers: "Organize imports", "Fill struct", "Extract
function", etc.

### UX contract

- A code action indicator appears when the caret is on a line with diagnostics
  and the server reports actions for that range.
- The indicator is a small button (lightbulb or `Fix` label) in the gutter or
  inline next to the diagnostic squiggle.
- Clicking the indicator or pressing **Cmd+.** opens a menu listing available
  actions, grouped by kind (quickfix, refactor, source).
- Choosing an action applies its edit (or runs its command) immediately.
- If the action has `edit.changes`, apply the workspace edit to the buffer and
  save. If `command` is present, execute the command (most commands in gopls
  return `workspace/executeCommand` which may trigger edits).

### Implementation map

**LSP protocol** (`internal/lsp/protocol.go`):
- Add `CodeActionProvider` `json.RawMessage` to `serverCapabilities`.
- The provider is `{codeActionKinds: string[], resolveProvider: bool} | bool`.
- `hasCodeActions()` — true when truthy.
- `CodeAction` struct: `Title`, `Kind`, `Diagnostics []Diagnostic`,
  `Edit *WorkspaceEdit`, `Command *Command`, `Data json.RawMessage`.
- `CodeActionParams` for the request: `TextDocument`, `Range`, `Context`
  (`Diagnostics`, `Only`).

**LSP client** (`internal/lsp/client.go`):
- `CodeActions(ctx, uri string, r Range, diags []Diagnostic, only []string) ([]CodeAction, error)` —
  call `textDocument/codeAction`.
- `CodeActionResolve(ctx, action *CodeAction) (*CodeAction, error)` — call
  `codeAction/resolve` when `resolveProvider` is true.
- `ExecuteCommand(ctx, command string, args []any) error` — call
  `workspace/executeCommand`.
- `ApplyWorkspaceEdit(ctx, edit *WorkspaceEdit) error` — call
  `workspace/applyEdit`.
- `HasCodeActions()` accessor.
- In `initialize` capabilities: `"codeAction": {"dynamicRegistration": false,
  "codeActionLiteralSupport": {"codeActionKind": {"valueSet": ["quickfix",
  "refactor", "source"]}}, "resolveSupport": {"properties": ["edit", "command"]},
  "dataSupport": true}`.

**Workspace edit application** (`internal/lsp/workspace_edit.go` — new):
- `WorkspaceEdit` struct: `Changes map[string][]TextEdit` (URI → edits).
- `TextEdit` struct: `Range`, `NewText`.
- `applyWorkspaceEdit(edit *WorkspaceEdit)` — for each URI:
  - `file://` URI → open the doc (or load into a temp buffer), apply `TextEdit`s
    to the buffer (backwards to avoid offset shifts), mark dirty.
  - If the file isn't open, read it from disk, apply edits, write back.
- For `Command` actions: `ExecuteCommand` then poll for `workspace/applyEdit`
  response from the server.

**App layer** (`internal/app/code_actions.go` — new):
- `fetchCodeActions(d *doc, line int)` — request actions for the current line
  or selection range, with the diagnostics at that position.
- Cache per `(line, version)` — actions change when the buffer or diagnostics do.
- `codeActionMenu(d *doc, line int)` — build the action menu UI.
- `applyCodeAction(action *CodeAction)` — apply edit or execute command.

**Editor integration** (`internal/app/editor_area.go`):
- Code action indicator: a small lightbulb icon or `Fix` button in the left gutter
  when the caret line has diagnostics AND code actions are available.
- The gutter mechanism: check if `editor.State` supports gutter icons/annotations.
  If not, a floating `Fix` button could overlay the editor (like inlay hints).
- Keyboard: **Cmd+.** (or **Ctrl+.**) triggers the action menu.
- Menu: a popup list below the caret, like signature help but vertical.

**Command palette** (`internal/app/commands.go`):
- `Show Code Actions` command.

### Test entry points

- `internal/lsp/lsp_test.go` — `fakeServer` add `textDocument/codeAction` case
  returning actions with edits and commands; test `HasCodeActions`, `CodeActions`,
  `CodeActionResolve`, `ExecuteCommand`, `ApplyWorkspaceEdit`.
- `internal/lsp/workspace_edit_test.go` — test `TextEdit` application to a buffer,
  multi-URI edits, and ordering.
- `internal/app/harness_test.go` — `fakeClient` add `codeActions` field.
- `internal/app/code_actions_test.go` — test fetch, menu construction, action
  application, and the no-server fallback.

### Gotchas

- **gopls code actions**: gopls returns `quickfix` (fix compile errors),
  `source.organizeImports`, `source.fixAll`, `refactor.extract`, `refactor.inline`,
  `refactor.rewrite`. Most use `edit`, not `command`.
- **Command execution**: some servers (typescript) use `command` for actions that
  trigger `workspace/executeCommand`. The server may respond by asking the client
  to apply an edit — the client must handle `workspace/applyEdit` as a server→client
  request. Check if `conn` handles this.
- **`workspace/applyEdit` direction**: this is a server→client request, not
  client→server. The client must register a handler and respond. Check how
  `conn.go` handles incoming requests vs. notifications.
- **Text edits ordering**: apply edits to a buffer in reverse document order
  (last line first) so offsets don't shift.
- **Non-open files**: `WorkspaceEdit` may touch files not open in tabs. Apply
  edits to the on-disk content and don't open tabs unless necessary.
- **Code action resolve**: some servers return actions without `edit`/`command`
  and require `codeAction/resolve` to populate them. Handle this lazily — resolve
  only the action the user picks.
- **Diagnostics dependency**: `CodeActionParams.Context.Diagnostics` filters
  which diagnostics the action addresses. Use the doc's current diagnostics for
  the caret line.

---

## Cross-cutting

### LSP capability negotiation

All three features need client capability registration in `initialize`:
- `semanticTokens`: `{requests: {range: true, full: true}, tokenTypes: [...],
  tokenModifiers: [...], formats: ["relative"]}`
- `documentLink`: `{tooltipSupport: true}`
- `codeAction`: `{codeActionLiteralSupport: ..., resolveSupport: ..., dataSupport: true}`

Server capabilities (`serverCapabilities`):
- `semanticTokensProvider`: `{legend, full, range}`
- `documentLinkProvider`: `{resolveProvider} | bool`
- `codeActionProvider`: `{codeActionKinds, resolveProvider} | bool`

### Fetch pattern

All three follow the same async fetch pattern:
- Trigger: buffer change, scroll (for semantic tokens), caret on diagnostic line
  (for code actions), document open (for document links)
- Goroutine → `client.Method(ctx, ...)` → `a.post.Post` → store on `doc`
- Stale-check: `a.gen`, `d.buf.Version()`, path match
- Debounce / key: per `(line, version)` or `(first, last, version)`

### Editor integration challenges

All three need to render something in the editor that the regex tokenizer doesn't:
- Semantic tokens: replace/override token coloring
- Document links: underline + hit-testing
- Code actions: gutter icon or inline button + popup menu

mygo's editor widget may not support all of these natively. If it doesn't:
- Semantic tokens: add a `SetTokens`/`SetOverlayTokens` API to `editor.State`
- Document links: add `SetLinks`/`SetLinkRanges` + click callback
- Code actions: add `SetGutterIcons`/`SetLineAnnotations` or use the existing
  `LineBackgrounds` + a custom draw pass

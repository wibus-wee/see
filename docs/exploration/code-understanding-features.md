# Code understanding: feature candidates

See reads code. This document proposes six features that make reading an unfamiliar
codebase faster, ranked, each specified well enough for a developer who has never seen
this discussion to implement it without asking what was meant.

It describes `main` at `4fb1a68` (release 0.2.4). Every claim about what See supports was
checked against that tree; file and line references point into it.

**Revision.** The first version of this document claimed the MyGo editor could not draw text
inside a line, and specified candidate 6 as an app-side overlay because of it. That was
wrong: `SetInlays` exists in the MyGo version See pins (`v0.9.0`, `go.mod:7`), and
`feat/ux-batch-1` already drives it for blame and inline diagnostics. §2.4, candidate 6 and
[§4.4](#44-decisions-the-owner-has-to-make) item 4 are corrected here; no other candidate
changed.

**Nothing here is in flight.** PR #56 (`feat/ux-batch-1`), #57
(`devin/1795065831-branch-diffs`) and #37 (`workspace-window-states`) already carry the
symbol outline, jump-bar symbols, peek definition, go-to-implementation, counterparts,
minimap, blame, bookmarks, folding and branch comparison. [§4](#4-why-this-order) lists
them so that no candidate below proposes the same work twice.

Mechanics in §1 are written from using these tools, not quoted from their documentation.

## 1. How the best reading tools do it

| Tool | Its reading model | What is worth copying |
| --- | --- | --- |
| Xcode | The symbol in front of you has a documentation card and a set of graph edges, both reachable from one control | Persistent inspector next to a transient popover; graph edges attached to the symbol, not to a menu of commands |
| Sourcegraph | References grouped by file with a context line, in a panel that stays put | Grouping and expansion instead of pagination; the subject of the panel is switchable |
| VS Code | Two kinds of orientation (cursor breadcrumbs, sticky scope) and lazily expanded trees | One level per request; incoming and outgoing in one view |
| Understand (SciTools) | A graph you can re-center on any node, with depth limits | Re-centering as the primary gesture; depth as a control |
| Sublime Merge, Gitea blame | Attribution is per line and the commit is one click away | Per-line facts, one click to the record behind them |

### Xcode

- **Quick Help** — Option-click a symbol, or open the Quick Help inspector
  (Option-Command-2) and leave it open. One card: declaration, abstract, availability
  and links into the documentation archive. It has two homes on purpose: a popover you
  summon and dismiss, and an inspector that stays while you read.
- **Jump to Definition** — Control-Command-J, or Command-click in the editor.
- **Show Callers and Show Callees** — the jump bar's Related Items menu lists Callers,
  Callees, Counterparts, Superclasses, Subclasses and Protocol Implementations.
  Choosing one *retargets the jump bar* at that symbol rather than opening a separate
  window, so a chain of calls is walked with a single control.
- **Show Counterparts** — switches to the paired file (header to implementation). For a
  reader this is "the other half of this code" in one keystroke.
- **Jump bar** — the path and the symbol path, each segment a popup listing its siblings.

Worth copying for a reader: one persistent place for documentation and one cheap
transient place; graph edges living on the symbol you are looking at; re-centering as a
first-class move.

### Sourcegraph

- Hover on a symbol gives a card with the signature and the doc, plus two actions: go to
  definition and find references.
- The **reference panel** pins references beside the code, grouped by file, each group
  showing the symbol's own line as context and expanding in place when a file has many.
  The file's symbol list sits next to it, so the subject of the panel changes without
  losing the panel.
- Symbol search is filtered at the query level, by kind and by file.

Worth copying: group by file, one context line per reference, expand rather than
paginate, and keep the panel alive while the code under it changes.

### VS Code

- **Peek Definition** (Option-F12) opens an inline popover with the definition body,
  navigable inside it.
- **Find All References** (Shift-F12) is also a peek: per-file groups, one context line,
  "N more" to expand a long group.
- **Call Hierarchy** (Shift-Alt-H) is a view, not a peek: two directions, Incoming and
  Outgoing, whose nodes expand one level per click.
- **Breadcrumbs** (Control-Shift-.) show the cursor's path including symbols, each
  segment a dropdown of its siblings.
- **Outline** lists document symbols hierarchically with kind glyphs.
- **Sticky scroll** pins the innermost enclosing declaration to the top of the editor
  while the body scrolls.

Worth copying: lazy expansion (no request for a graph that nobody has opened yet), both
directions in one view, and the separation between the cursor's path and the viewport's
enclosing scope — they are different questions.

### Understand (SciTools)

- **Butterfly** draws one entity's callers above it and callees below it in a single
  two-sided tree; clicking any node re-centers the whole view on that node.
- Call trees and reference tables expand with explicit depth limits.
- Dependency and architecture browsers work at file and module level.
- Metrics per entity tell a reader which of a hundred functions is worth reading first.

Worth copying: re-centering (a reader's real move, and the one See cannot make today),
and depth as something the reader controls rather than a fixed answer.

### Sublime Merge, Gitea and other history browsers

- File history lists the commits touching one path, newest first, each opening its diff.
- Blame attributes every line to a commit; the commit and that hunk's diff are one click
  from the line.
- Review happens per hunk, and comparisons happen between refs.

Worth copying: facts are per line, and the record behind a line is one click away.

### What the benchmark reduces to

1. **Understanding is graph walking.** Definition, references, callers, callees,
   implementations: all attached to the symbol in front of you, all re-centerable.
2. **Reference material needs two homes.** A popover that dies on the next click, and a
   persistent surface that survives scrolling and clicking. See has only the first.
3. **Expand lazily, one level per request.** No tool fetches the graph; they fetch the
   next level around the node the reader opened.
4. **Orientation is a feature.** Where the cursor is and which declaration the viewport
   is inside are separate answers, and good readers use both.

See already copies rule 1 for one direction (references, incoming calls) and rule 2 not
at all. §3 is organised around the rest.

## 2. What See does today

### 2.1 Language server requests

`internal/lsp/client.go:172` declares support for document symbols, references, call
hierarchy, definitions and hover when it initializes a server, and reads back only
position encoding, text-document sync, `definitionProvider` and `diagnosticProvider`
(`internal/lsp/protocol.go:112`).

| LSP request | Client method | Driven from | Notes |
| --- | --- | --- | --- |
| `textDocument/definition` | `Client.Definition` (`internal/lsp/client.go:249`) | Command-hover with the code in the card (`internal/app/hover.go:126`), Go to Definition (`internal/app/app.go:1063`) | Locations parsed by `ParseLocations`; takes the first target |
| `textDocument/hover` | `Client.Hover` (`client.go:263`) | `hoverAt` (`internal/app/hover.go:83`) | Markdown, cached for 10 s up to 128 entries (`hover.go:54`) |
| `textDocument/references` | `Client.References` (`client.go:296`) | `findUsagesAt` (`internal/app/app.go:1508`) | Not capability-gated |
| `textDocument/prepareCallHierarchy`, `callHierarchy/incomingCalls` | `Client.Callers` (`client.go:303`) | `findUsagesAt` (`app.go:1500`) | Flattened to `[]Location` (`client.go:308`); the hierarchy item is discarded; outgoing calls never requested |
| `textDocument/documentSymbol` | `Client.DocumentSymbols` (`internal/lsp/document_symbols.go:21`) | Search File Symbols (`internal/app/file_symbols.go:52`) | `flattenDocumentSymbols` (`document_symbols.go:29`) drops nesting, `kind` and the full range |
| `workspace/symbol` | `Client.WorkspaceSymbols` (`client.go:158`) | `Cmd+T` and `#` in Open Quickly (`internal/app/quickopen.go:52`) | |
| `textDocument/diagnostic` (pull) | `Client.Diagnostics` (`client.go:283`) | Diagnostics of open documents | Alongside pushed diagnostics (`client.go:394`) |

These requests have no occurrence anywhere in `internal/lsp`: `textDocument/signatureHelp`,
`callHierarchy/outgoingCalls`, `textDocument/implementation`, `textDocument/typeDefinition`,
`textDocument/prepareTypeHierarchy`, `textDocument/inlayHint`, `textDocument/codeAction`,
`textDocument/formatting`, `textDocument/rename`. The last three are editing services and
belong nowhere in See; the first six are reading services a reader misses.

### 2.2 Servers See starts

`internal/languages` starts gopls for Go (`languages.go:65`), tsrs for TypeScript and
JavaScript, clangd for C and C++, and cmake-language-server for CMake. Starting one
requires a trusted workspace (`internal/app/app.go:1465`). Which requests a server
answers varies, so every new request needs an unsupported state, not an error state.

### 2.3 The surfaces that exist

| Surface | Where | What it does | What it cannot do |
| --- | --- | --- | --- |
| Hover card | `showHover` (`internal/app/hover.go:68`), `renderHover` (`hover.go:178`) | Markdown documentation for the symbol under the pointer, at most 620 pt wide | Dies when the pointer moves; links inside it are inert (`markdown.View(c, md, nil)`, `hover.go:201`) |
| Command-hover | `hover.go:125` | Underlines the symbol and shows the definition's own code in the card | One definition, one level |
| Related Items menu | `jumpBar` (`internal/app/tabbar.go:193`), menu at `:207` | Two entries: Callers, References | No callees, no implementations; the result replaces the code |
| Definition and reference peek | `editorPeek` (`internal/app/editor_area.go:584`) with `defChoices` (`app.go:210`) and `relatedView` (`editor_area.go:501`) | Popover anchored at the caret: code preview on the left, results grouped by file on the right, collapsible per file, single click to preview, double click to open | One level only; closed by any other click; no filter; no pin |
| Jump bar | `tabbar.go:193` | The path's folders; clicking one reveals it in the navigator | No symbols |
| Navigator sidebar | `navigatorPane` (`internal/app/navigator.go:21`), `navFiles`/`navFind`/`navGit`/`navProblems` (`app.go:144`) | Files, Find, Git, Problems | No symbols, no reference list |
| Bottom panel | `panelView` (`internal/app/terminal.go:22`), drawn at `:376`, tabs at `:427` | Terminal, Output, Tasks | No reading surface |
| Open Quickly | `quickOpenPanel` (`internal/app/quickopen.go:18`), modes at `:50` | `>` commands, `@` file symbols, `#` workspace symbols, `:` line | Symbol modes are flat lists without kind or nesting |
| Problems | `problemLocations` (`internal/app/problems.go:31`) | Diagnostics of open documents, filtered by path, message or source | Read-only report; nothing to do with them |

### 2.4 What the editor can draw

The editor is MyGo v0.9.0 (`go.mod:7`). It offers the app six ways to influence what the
text looks like, and one thing it does not offer. Every editor-side candidate below is
scoped around the boundary.

| Mechanism | Where | What it gives |
| --- | --- | --- |
| Line band | `LineBackgrounds` | a colour behind a run of lines; #56's inline diagnostics use it |
| Text band | `TextBackgrounds` | a colour behind a byte range; the Command-hover link uses it (`internal/app/hover.go:74`) |
| Popover | `ShowHover` | content anchored at a range (`hover.go:77`) |
| Inlay text | `SetInlays([]Inlay)` (`ui/editor/state.go:478`), `Inlay{Offset, Text, Trailing, Ellipsis}` (`ui/editor/lsp.go:90`) | text drawn inside a line that is not in the buffer; `Trailing` keeps it after the code as a diagnostic or a blame does, `Ellipsis` shortens it to the room left; it moves with the edits until set again |
| Replacement ranges | `SetReplacements([]Replacement)` (`ui/editor/state.go:497`) | a byte range shown as something else, or as nothing: folded lines, hidden Markdown marks |
| Callbacks | `OnHover`, `OnDefinition`, `OnCompletion`, `OnGutterClick`, `OnInput` (`ui/editor/state.go:96` to `:138`) | events from the editor into the app |

One thing is missing: a callback for the caret moving. Candidate 3 therefore triggers from
the frame loop instead of a callback. Candidate 6 needs no workaround at all — the inlay lane
is the editor's own, and `feat/ux-batch-1` already drives it: `d.languageInlays
[]editor.Inlay` (`app.go:213`), merged with inline diagnostics and blame in `editorInlays`
(`internal/app/blame.go:86`) and applied at `editor_area.go:295`. `blame_test.go:20` on
that branch sets a language inlay with `Text: "type hint"`, which is the lane this candidate
fills.

### 2.5 Gaps the candidates work from

- **G1 — One-directional graph.** Callers are available; callees, implementations and
  type hierarchy are not (`client.go:303` is the only call-hierarchy request).
- **G2 — Reference material is transient.** Hover and the reference peek both die on the
  next click; nothing in the window holds documentation or a result list (`hover.go:68`,
  `editor_area.go:379`).
- **G3 — Symbols are flattened.** Nesting, kind and full ranges are dropped
  (`document_symbols.go:29`), so no surface can draw a tree or know where a declaration
  ends.
- **G4 — Call sites are unexplained.** No signature help, so reading `f(ctx, x, true, 3)`
  means leaving to open `f`.
- **G5 — No viewport orientation.** The jump bar names the file and its folders and stops
there; in a 600-line file nothing says which declaration the screen is inside.
- **G6 — Inferred types are invisible.** `x := compose(a, b)` shows no type unless the
  reader opens `compose`.
- **G7 — A result list cannot be kept.** The references peek offers no filter, no pin and
  no survival past the next click (`relatedView`, `editor_area.go:501`).

## 3. Candidates

Ranked by what a reader needs first in an unfamiliar repository, then by cost. Sizes are
S (a day or two), M (about three days), L (about a week) of work by one developer who
knows the file.

| # | Candidate | Size | Answers |
| --- | --- | --- | --- |
| 1 | Call hierarchy panel | L | Who calls this, and what does it call |
| 2 | Quick Help inspector | M | What does this symbol mean |
| 3 | Signature help at the call site | M | What are these arguments |
| 4 | Pinnable references panel | L | Where else is this used |
| 5 | Reading-position pin | S–M | Where am I in this file |
| 6 | Inlay type hints | S–M | What type is this, really |

---

### 1. Call hierarchy panel

**Size: L.** New bottom-panel view with an expandable tree in both directions.

**(a) What the reader does**

You are reading `internal/app/app.go`, stop on `findUsagesAt`, and two questions decide
whether you keep reading or leave: who calls this, and what does it call on the way down?
Today See answers the first by covering the editor with a popover listing call sites —
one level, no way onward, gone as soon as you click into the code. What you want is a tree
you can walk: up to the handlers that reach it, down into the helpers it leans on, any node
expandable, the tree staying open beside the code while you read.

**(b) UI and interaction**

- **Home: the bottom panel, as a new view.** `panelView` (`internal/app/terminal.go:22`)
  gains `panelCalls`, rendered in the panel's switch (`terminal.go:380`) and listed in its
  tab strip (`terminal.go:427`). A panel rather than a popover because a tree needs height
  and must survive clicking into a file, and the panel already hosts non-terminal views
  (Output, Tasks).
- **Look.** A segmented control at the top of the view with two segments, Incoming and
  Outgoing, built with `ui.SegmentedBase` as the navigator bar is (`navigator.go:53`). Under
  it a tree: one row per symbol, 24 pt tall, 12 pt text, a kind glyph on the left (extend
  `symbolName` in `internal/app/symbols.go:18` with function, method, struct, interface and
  variable entries and reuse `symbolIcon`), the name, then `file:line` in the muted colour on
  the right, and a disclosure triangle on rows that can expand.
- **Root.** The symbol the request started from is the first row, in the normal text
  colour, with its file and line. Its children appear one level deep. A node that has not
  been opened has no children and asks the server only when opened.
- **Mouse and keyboard.** Single click selects a row and previews that position in the
  peek code view that `relatedView` already uses (`editor_area.go:501`), so the reader can
  see the call site without leaving the panel. Double click opens the file at the position.
  Right-click offers **Make Root** (re-centers the tree on that node, as SciTools'
  Butterfly does) and **Copy Location**. Up and Down move the selection, Left and Right
  collapse and expand, Return opens.
- **Entry points.** A third item, **Call Hierarchy**, in the jump bar's Related Items menu
  (`internal/app/tabbar.go:207`); the command palette entry **Show Call Hierarchy** on
  Shift+Alt+H (VS Code's binding; free in `internal/app/commands.go`); and a row action in
  the references peek later, once candidate 4 lands.
- **Empty, loading and error states.**
  - No language server for the file: the existing note
    (`locales.MsgNoLanguageServerIsConfiguredForThisFile`, used at `app.go:1462`).
  - Untrusted workspace: the existing trust prompt and retry-after-trust path
    (`app.go:1465`), which re-runs the request once trusted.
  - A level loading: a spinner row in the place of that node's children, as the find
    navigator shows while listing (`navigator.go:119`). The rest of the tree stays usable.
  - No calls: one muted row, **No callers found** / **No callees found**, distinct from
    "the server does not answer this direction", which gets its own row: **This language
    server does not report outgoing calls**. That second state is required, not optional,
    because server support varies.
  - Server error: a row with the error text and **Retry**, in `colors(c).Err`, following
    the listing error in `navigator.go:112`.
  - Stale replies: every level is stamped with the document version and the app generation
    (`a.gen`, `a.navSeq`) and dropped if either moved, as `findUsagesAt` does
    (`app.go:1484`, `app.go:1516`).
- **Fit with the rest of the window.** The view sits where Output sits, with the same
  separators, sidebar-free background and 1 px borders; nothing is rounded or shadowed
  because it is part of the window rather than floating over it.

**(c) Why it serves reading rather than editing**

It answers questions about a symbol without moving the reader, and re-centering keeps the
*question* rather than the cursor as the thing that persists. Nothing in it proposes a
change to the code, and none of the information it shows (who calls this, what this calls)
is worth showing to someone who is only editing one function they already know.

**(d) Implementation sketch**

- `internal/lsp/client.go` — add the three primitives rather than one convenience call:
  `PrepareCallHierarchy(ctx, uri, pos) ([]CallItem, error)`,
  `IncomingCalls(ctx, item) ([]CallEdge, error)`, `OutgoingCalls(ctx, item) ([]CallEdge, error)`,
  with `CallItem{ID, Name, Kind, Detail, URI, Range, SelectionRange}` and
  `CallEdge{Item CallItem, FromRanges []Range}` in `internal/lsp/protocol.go`. Keep
  `Callers` (`client.go:303`) as a thin wrapper over the new primitives so
  `app.go:1500` does not change. This is a refactor, not an addition: `Callers` flattens
  edges into `[]Location` (`client.go:308`) and throws away the item needed to expand.
  Probed against gopls v0.23.0 on a throwaway module: `prepareCallHierarchy` on a function
  name returns the item and `callHierarchy/outgoingCalls` answers with real callees
  (`main` → `compose` and `fmt.Println`; `compose` → `helper`), so the direction the panel
  needs does exist. Two shapes to handle as they actually arrive: one edge can repeat the
  same line in `fromRanges` when a function is called twice on that line (merge by line, or
  the row lists one call site twice), and a position that is not a callable answers with an
  error envelope — `code: 0, message: "no symbol here"` — rather than an empty array, so
  that case maps to the empty state and not to `lspFailure`.
- `internal/lsp/protocol.go` — add `CallHierarchyProvider json.RawMessage` to
  `serverCapabilities` (`:112`) and `hasCallHierarchy()` beside `hasDefinition()` (`:144`).
  Today no call-hierarchy capability is read and an unsupported server produces an error
  through `lspFailure` (`app.go:1521`); the panel needs an unsupported *state*.
- `internal/app/calls.go` (new) — `callTree{root, direction, nodes, expanded, loading,
  seq, cancel}`, `openCallHierarchy(anchor)`, `expandCallNode(id)`, `makeRoot(id)`. The
  worker goes through `a.serverFor(path)`, `m.Get(ctx)` and `m.Sync(ctx, c, d.uri, snap)`
  exactly as `findUsagesAt` does (`app.go:1485` to `:1513`), with `a.navCancel` for
  cancellation (`app.go:1477`) and `a.post.Post` to return to the UI thread.
- `internal/app/terminal.go` — `panelCalls` in `panelView` (`:22`), the render case
  (`:380`), and the tab strip (`:427`). The strip assumes terminal tabs, so the Calls row
  must not take a tab index: follow how Output and Tasks rows are handled (`:468`, `:474`).
- `internal/app/tabbar.go` — the third Related Items item (`:207`).
- `internal/app/commands.go` — `{"Show Call Hierarchy", "Shift+Alt+", "H", ui.Shift | ui.Alt,
  ui.KeyH, ...}`; `internal/app/view.go` — `none(ui.Shift|ui.Alt, ui.KeyH)` in
  `editorBindings()` (`:19`) so the editor does not take the key first.
- `internal/app/command_locale.go`, `locales/en.json`, `locales/zh-Hans.json`, then
  `go tool mygo generate` to regenerate `locales/catalog_gen.go`. `TestCommandTranslations`
  (`internal/app/command_locale_test.go:8`) fails until both catalogs carry the label.
- Layer rule: `internal/arch/arch_test.go:48` keeps `github.com/egoist/mygo` out of every
  package except `internal/app` and `internal/platform`, so the tree state and its rendering
  live in `internal/app`, not in `internal/lsp`.
- Tests: `internal/lsp/calls_test.go` against the fake server the package already uses
  (`internal/lsp/lsp_test.go`), covering hierarchical edges, an unsupported server and a
  cancelled request; `internal/app/calls_test.go` on the harness
  (`internal/app/harness_test.go:28`) with a fake `langserver.Client`, covering lazy
  expansion, a stale reply after a document change, and the unsupported row; a real-gopls
  case in `internal/app/real_gopls_test.go`
  for one Go function with a known caller and callee.

**(e) Size and work breakdown**

L, about 5 days: protocol and client primitives half a day; tree state, worker and stale
guards a day and a half; panel view, rows, keyboard two days (it is the first reading view
in the panel); entry points, locales and settings half a day; tests a day.

**(f) Risks and unknowns**

- **Outgoing calls per server.** Verified for gopls v0.23.0 only, in a real LSP session
  against a throwaway module (see (d)). tsrs and clangd were not probed, so if a server
  answers one direction only, the panel must say so per direction rather than show an empty
  tree.
- **`Callers` has no test today** (nothing outside `internal/app` and `internal/lsp`
  references it) and it loses the hierarchy item, so this candidate touches a code path
  with no safety net.
- **Conflicts with #56.** That branch changes `internal/app/terminal.go` substantially for
  terminal session restore; expect a conflict in the panel's tab strip.
- **Focus.** The panel is the terminal's territory (`tp.focused`, `terminal.go:397`).
  Keyboard handling in the Calls view must not take keys from a terminal tab next to it.
- **Latency.** `prepareCallHierarchy` on a large Go method can take hundreds of
  milliseconds; every level must be cancellable and must never block the previous tree
  from staying on screen.

---

### 2. Quick Help inspector

**Size: M.** A persistent documentation pane beside the editor.

**(a) What the reader does**

You are reading a file and the doc comment for the symbol you care about is a screen up,
or in another file, or both. Hovering shows it for as long as the pointer stays still,
which is exactly as long as you are not reading. You want the documentation to stay put
while you read the code below it, and to follow you as you move through the file.

**(b) UI and interaction**

- **Home: a pane on the right edge of the editor area**, opened and closed as one unit,
  sharing the window's height with the editors. Not a sidebar segment: the sidebar holds
  navigation, and the in-flight outline and bookmarks work already claims two of its
  slots. Not the bottom panel: it docks low and takes the height that code needs. The
  editor area's right edge is where Xcode puts its inspectors.
- **Width** 280 pt by default, draggable between 220 and 420, persisted next to
  `SidebarWidth` (`internal/app/app.go:247`, `internal/config`).
- **Contents**, top to bottom: the symbol's name and kind; the declaration as code,
  monospaced with the editor's syntax colours where the line is available; the
  documentation, scrollable and selectable; a source row `internal/git/history.go:112`
  that opens the file when clicked; and a row of actions — **Callers**, **References**,
  **Call Hierarchy** — that hand the symbol to candidates 1 and 4 instead of opening a
  competing view.
- **Following and pinning.** The pane follows the caret's symbol, debounced by about
  250 ms so that dragging the caret across a file does not fire a request per character.
  A lock button at its top right pins the current symbol: the pane then ignores the caret
  until unlocked or until **Follow** is pressed. The pinned text is the whole point — a
  reader comparing two functions keeps both in view.
- **Entry points.** Command **Show Quick Help** on Option-Command-2 (Xcode's Quick Help
  inspector binding; free in `commands.go`), a toggle button at the right end of the jump
  bar, and Settings → Editor → **Quick Help Inspector** (off by default, because it costs
  code width).
- **Empty, loading and error states.** No editor in focus: a short hint, **Put the caret
  on a symbol**. No server for the language, or an untrusted workspace: the existing note
  and trust path. Loading: the previous symbol's content stays and a 1 pt progress line
  appears under the pane's header — never a blank pane, because a pane that blanks on
  every caret move is worse than stale text. Documentation absent but a declaration
  known: the declaration alone, from `definitionPreview` (`hover.go:207`). Documentation
  present but the symbol unresolved: a muted **No documentation for this symbol**.
  Server error: one muted line with the reason, and the last good content kept.
- **Fit.** Sidebar-coloured ground, 12 pt text, the same 1 px `colors(c).Separator` rules,
  no shadow and no rounding: it is part of the window, unlike the hover card
  (`hover.go:180`), which floats.

**(c) Why it serves reading rather than editing**

It takes the pointer out of the loop: reading is a keyboard activity, and every
documentation tool that requires aiming a mouse at a token interrupts it. It also makes
documentation for code you never open available next to code you are reading, which is the
common case when a repository's comments explain more than its names do.

**(d) Implementation sketch**

- `internal/app/quickhelp.go` (new) — `quickHelp{pinned bool, subject quickHelpSubject,
  seq, cancel, last good content}`, the follow decision, and the rendering described above.
- `internal/app/hover.go` — factor the request half of `hoverAt` (`hover.go:83` to `:166`)
  into `hoverAtOffset(ctx, d, st, offset) (hoverResult, error)` so the popover and the pane
  share one path, one cache and one set of encodings. The existing cache is keyed by
  `hoverKey` with a 10 s TTL and 128 entries (`hover.go:35`, `:54`); pinned entries need
  their own key suffix or a longer TTL, otherwise a pinned doc expires while it is on
  screen.
- `internal/app/editor_area.go` and `internal/app/view.go:119` — the editor area lays out
  its groups in a `ui.Row`; add the pane as a third child with a draggable divider, the way
  the sidebar width is handled. Its visibility and width go into `Config`
  (`internal/config/config.go`) and, if it should survive a restart, into the session
  (`internal/config/session.go`).
- `internal/app/hover.go:184` `hoverContent` — extract one content builder used by both the
  hover card and the pane, so a change to how declarations are shown happens once.
- Links inside documentation. `markdown.View(c, md, nil)` (`hover.go:201`) passes no link
  handler, so every link in a doc comment is inert. The pane should pass one: `http` and
  `https` targets open through `services.OpenURL` (as `links.go:127` does), and relative
  targets are left inert with a tooltip until there is a resolver. Go's doc comments link
  types as bracketed names; gopls emits those as markdown links, so resolving them through
  `workspace/symbol` (`client.go:158`) is the obvious follow-up, not part of this item.
- Tests: `internal/app/quickhelp_test.go` on the harness — the pane shows declaration and
  doc; a stale reply for the previous caret position is dropped; a pinned pane survives a
  caret move; the pane keeps its content while a new request is in flight.

**(e) Size and work breakdown**

M, about 3 days: state, follow, debounce and stale guards a day; layout, divider and
persistence a day; content builder, declaration line and link handling half a day; tests
half a day.

**(f) Risks and unknowns**

- **Screen real estate.** On a 13-inch laptop the pane costs a quarter of the code width.
  Off by default, one click to toggle, and the width is remembered per window.
- **A pane that chases the caret is annoying** if it flickers. The debounce and the
  keep-the-previous-content rule matter more than anything the pane draws.
- **Splitting the hover blob.** gopls returns the signature and the doc as one markdown
  blob, so "declaration on top, doc below" is a heuristic on the first fenced block, and
  it will be wrong for some servers. The fallback is to show the blob unchanged.
- **Conflicts with #56.** That branch rewrites the jump bar (`internal/app/jumpbar.go` on
  `feat/ux-batch-1`) and edits `editor_area.go`, which is where the toggle and the pane
  land.

---

### 3. Signature help at the call site

**Size: M.** The parameter list of the call the caret is inside.

**(a) What the reader does**

You read `git.Command(ctx, root, "for-each-ref", "--sort=-committerdate", "refs/heads")`.
Nothing on the line says whether `root` is the repository or the directory git runs in,
whether the flags are See's or git's, or which parameter you would change to sort the other
way. To learn that you either open `Command` and lose your place, or guess — and the same
doubt applies to every call in the file. See should tell you, in place, without a request
you have to make: a one-line strip under the call showing the signature and marking the
parameter the caret is in.

**(b) UI and interaction**

- **Home: a strip inside the editor, directly under the line holding the caret**, drawn
  the way the change and reference peeks already position themselves against a line
  (`editorPeek`, `editor_area.go:584`, which places content below the line or above it when
  there is no room below).
- **When it appears.** Whenever the caret sits inside a call's argument list — a bracket
  depth greater than zero relative to the call being read — not only while typing. That is
  the whole difference between an editor's parameter hints and a reader's.
- **Contents.** The signature in 12 pt monospace, with the active parameter emphasised;
  `1/2` when the server returns overloads, switched with Up and Down; one line of
  parameter documentation underneath when the server sends it. The label comes from the
  server, not from parsing the definition, so generics and method sets stay correct.
- **Keys and dismissal.** Control-Shift+Space shows it by hand (VS Code's binding; free in
  `commands.go`). Escape dismisses it for that position only. Moving the caret out of the
  call closes it silently.
- **Settings.** Settings → Editor → **Parameter Help**: Always, On Trigger, Off. Default
  Always, because a reader did not ask for the call and should not have to ask for its
  parameters; the setting exists for files that are dense with calls.
- **Empty, loading and error states.** Silence is the correct state for every failure
  here: no server, an unsupported server, a slow server that answers after the caret
  moved, or a parse error all produce nothing, never a message. A strip that complains is
  worse than no strip, and the reader has not asked a question they can see.
- **Fit.** The strip borrows the hover card's material — `colors(c).Popover` ground, a 1 px
  `PopoverEdge` border, the hover card's soft shadow (`hover.go:180`) — at 22 pt tall and
  the full width of the line's text, so it reads as a footnote to the code rather than as
  a window.

**(c) Why it serves reading rather than editing**

A call's meaning is its parameters, and the current workaround (open the definition, read
the signature, go back) throws away the reading position that See exists to protect. The
feature surfaces information the server already has at the exact place it is needed.

**(d) Implementation sketch**

- `internal/lsp/client.go` — `SignatureHelp(ctx, uri, pos) (*SignatureHelp, error)`
  mirroring `Hover` (`:263`); `protocol.go` — `SignatureHelp`, `SignatureInformation` and
  `ParameterInformation` types, tolerant of both forms of `parameters[].label` (a string or
  a `[start, end]` pair; the LSP union), the way `ParseHover` (`protocol.go:152`) tolerates
  three shapes of hover content.
- `internal/lsp/protocol.go:112` — `SignatureHelpProvider json.RawMessage` in
  `serverCapabilities` plus `hasSignatureHelp()`; and declare the capability in
  `initialize`'s `textDocument` block (`client.go:185` to `:192`):
  `"signatureHelp": {"signatureInformation": {"documentationFormat": ["markdown", "plaintext"],
  "parameterInformation": {"labelOffsetSupport": true}}}`. Without the declaration some
  servers answer a reduced form.
- `internal/app/signature.go` (new) — `signatureState{seq, cancel, path, version, offset,
  help, dismissedAt}`, the debounce, and a `signatureAt(d, st, offset)` request through
  `a.serverFor`/`m.Get`/`m.Sync` as elsewhere. Cache by (path, version, call start offset)
  so moving within one argument list does not re-ask.
- The trigger. There is no caret-change callback on the editor state today (§2.4): the
  editor reports `OnDefinition` and `OnHover` (`app.go:1063`, `:1076`) and nothing for a
  caret move. The caret position is already read every frame where `g.caretAnchor` is
  computed (`editor_area.go:386` to `:389`), so the cheapest correct trigger is a
  comparison there against the last value,
  which costs one integer compare per frame. A proper `OnCaret` on the editor belongs
  upstream in MyGo and is the clean version; the per-frame compare is the one that ships
  without touching a dependency.
- Drawing the strip: the app computes a line's top as `st.LineTop(line) - st.ScrollY()`
  and measures text with `c.MeasureText` (both already used at `editor_area.go:342` and
  `:388`), so the strip is positioned like a peek with a fixed height instead of a
  measured one.
- Tests: `internal/lsp/signature_test.go` on the fake server, covering one signature,
  overloads, a label-offset parameter list and a `null` result; `internal/app/signature_test.go`
  on the harness for the visible/hidden decisions, dismissal and a stale reply. No real
  gopls case: signature help depends on the caret being inside a call, which the harness
  can express with a fake client and cannot express cheaply against gopls.

**(e) Size and work breakdown**

M, about 2.5 days: protocol, client and tolerant parsing half a day; state, debounce,
cache and trigger a day; the strip's rendering half a day; tests half a day.

**(f) Risks and unknowns**

- **Server support is uneven.** clangd and tsrs answer signature help readily; gopls
  answers it for calls it has typed information for, and this work did not run gopls to
  check the read-time case (caret inside a call, no edit). Verify before committing to
  "Always" as the default; the setting makes a wrong default recoverable, not invisible.
- **A strip that appears and disappears shifts nothing and still distracts.** It should
  stay for as long as the caret is in the call, even across blank lines inside the
  argument list.
- **Overloads.** Where the server returns several signatures, marking the active one is
  the server's decision (`activeSignature`); See must not guess.
- **Per-frame requests would be a performance bug.** The debounce plus the (path, version,
  offset) cache is what keeps this cheap; both are required, not optional.

---

### 4. Pinnable references panel

**Size: L.** The references the peek already finds, kept, filtered and grouped.

**(a) What the reader does**

You have found `applyDiffPrefs` and want to know every place it is used before you trust
your reading of it. Shift+F12 gives you a popover that covers the code, lists forty results in
a fixed box, cannot be filtered, and closes the moment you click a result to look at it.
You want the list to stay in the window, filter as you type, and hold the last few lists
you asked for so you can go back to the symbol you were reading two minutes ago.

**(b) UI and interaction**

- **Home: the bottom panel, as a second reading view** beside the call hierarchy
  (candidate 1) — the same `panelView` switch (`internal/app/terminal.go:22`, `:380`) and
  the same tab strip (`:427`).
- **Getting there.** Shift+F12 keeps opening the peek it opens today
  (`internal/app/app.go:1066`). The peek's header gains a **pin** button next to its close
  button; pressing it moves that result list, unchanged, into the panel and closes the
  popover. The command palette also gets **Pin References**, which skips the popover. The
  peek stays the default for one-off questions, because for three results a panel is
  heavier than a popover, and the popover is already good at that.
- **Look.** A filter field at the top (path, symbol name or the matched line's text), a
  count — `37 references in 9 files` — beside it, then rows grouped by file exactly as
  `relatedView` groups them today (`editor_area.go:505` to `:512`): a file row with its
  relative path, a count and a collapse triangle, then one row per reference showing the
  matched source line, with the match emphasised.
- **The list is a reading queue.** Single click previews the reference in the peek
  editor (the behaviour `selectRelated` already implements, `app.go:1600`); double click
  opens the file at that line; Space marks a row as read and moves to the next, so a
  reader can walk sixty results without the mouse and without losing the list.
- **Recent lists.** A clock button at the left of the filter field holds the last five
  lists, each labelled by symbol and count, so a reader can return to a list they closed.
  This is the part the popover cannot do at any size.
- **Empty, loading and error states.** Waiting for the server: the previous list stays
  with a spinner in the header, never an empty panel. No language server for the file: the
  existing note, shown in the panel rather than as a transient banner. Untrusted
  workspace: the trust prompt. No references: `No references found` plus the one useful
  hint — that a server may not count references in files it has not opened, which is true
  of gopls for some queries. Error: a row with the message and **Retry**. Stale: the list
  is stamped with the document version and dropped if the document changed
  (`app.go:1516` is the pattern).
- **Fit.** Identical to candidate 1: sidebar-free ground, 1 px `colors(c).Separator`
  rules, 12 pt rows, selected row in `colors(c).SelectedRow`.

**(c) Why it serves reading rather than editing**

A reference list is the reading queue for "do I understand what this symbol touches".
Keeping it, filtering it and remembering it turns the current one-shot popover into
something a reader can work through at their own pace across several files — and the
reader never has to hold their place in a modal while doing it.

**(d) Implementation sketch**

- `internal/app/references.go` (new) — `referenceList{runs []referenceRun, active int,
  filter string, cursor int, read map[target]bool}`, `pinReferences(ch *defChoices)`,
  filtering, and the walk-with-Space behaviour. The result data is the `defChoices` the
  app already builds (`app.go:210`) with its targets and previews from `loadRelatedPreviews`
  (`app.go:1544`), so this candidate reuses the fetch path rather than adding one.
- The pin button. `editorPeek` (`editor_area.go:584`) takes `closeLabel` and `close func()`
  and draws one action; give it an optional second action (a small struct rather than two
  more parameters) and update its two call sites, the Git change peek (`:373`) and the
  references peek (`:384`). The Git change peek and the hunk-level review in #56 both use
  this function; keep the change additive.
- Extracting the list. `relatedView` (`editor_area.go:501`) already draws exactly these
  rows inside a popover. Factor the row-and-group drawing into one widget used by the
  popover and by the panel, and reuse it in candidate 1's tree rows; three surfaces drawn
  from one implementation is the reason to do candidates 1 and 4 in the same wave.
- `internal/app/terminal.go` — the second reading view in `panelView`, its render case and
  its tab-strip row, as in candidate 1.
- `internal/app/commands.go` — **Pin References**; no new shortcut, because Shift+F12
  already starts it.
- Locales as in candidate 1 (`locales/en.json`, `locales/zh-Hans.json`, `go tool mygo generate`,
  `command_locale.go`).
- Tests: `internal/app/references_test.go` on the harness — pinning moves the list, the
  filter narrows it, Space advances, a stale list is dropped after the document changes,
  and a second pin replaces the first list but keeps it in recents.

**(e) Size and work breakdown**

L, about 4 days: the shared list widget, extracted from `relatedView`, a day; panel view,
filter, keyboard walking a day and a half; pin button, recents and commands half a day;
tests a day.

**(f) Risks and unknowns**

- **Two ways to see references** is a real cost. The mitigation is that the popover stays
  the default and the panel is only reached by an explicit pin.
- **Partial results.** Reference counts depend on what the server has indexed; gopls can
  miss references in files it has not loaded. The panel must say "37 references found" and
  not "37 references exist".
- **Conflicts with #56.** That branch adds find-and-replace to the Find navigator and a
  bookmarks panel (`internal/app/find_replace.go`, `bookmarks.go` on `feat/ux-batch-1`),
  and edits the same panel and navigator files. Expect conflicts; they are local to the
  tab strip and the row widget.
- **Memory.** A list of previews is a buffer per file (`loadRelatedPreviews` builds one);
  cap the kept runs at five and the retained previews at, say, 500 references.

---

### 5. Reading-position pin

**Size: S–M.** The declaration the viewport is inside, above the code.

**(a) What the reader does**

You scroll through a 600-line file. The function you started reading left the top of the
screen two hundred lines ago and nothing on screen says which function the code in front
of you belongs to. You scroll back to check, lose the place you had, and scroll forward
again. The window should keep answering "which declaration am I inside" without you
asking.

**(b) UI and interaction**

- **Home: a 22 pt row directly above the editor's text, below the jump bar**, inside the
  editor area's column (the jump bar is drawn at `editor_area.go:84`). Not an overlay over
  the text: the row takes space in the layout, so no line is ever covered.
- **Contents.** The innermost enclosing declaration's name and kind, and above it, muted
  and truncated, up to one further enclosing level when the file nests (a method inside a
  type's block, a callback inside a function). Deeper nesting collapses to an ellipsis in
  front. Clicking any segment jumps to that declaration's line and puts the caret on its
  name.
- **When it appears.** Only once the enclosing declaration's own line has scrolled above
  the top of the viewport; at the top of a scope the row disappears. Add one row of
  hysteresis so that a scroll that sits exactly on the boundary does not flicker it.
- **Entry points.** Automatic. Settings → Editor → **Reading Position Pin**, on by
  default. No shortcut: it is a state of the window, not an action.
- **Empty, loading and error states.** All three are the same: no row, and the editor
  moves up by 22 pt. No server, no language for the file, an unsupported server, an empty
  file, or a first visible line above every declaration produce no row: no placeholder, no
  spinner and no message, because a permanent 22 pt strip that usually says nothing would
  cost reading space for no answer.
- **Fit.** The jump bar's visual language: editor-coloured ground, a 1 px
  `colors(c).Separator` line under it, 12 pt text, muted for the outer scope and normal for
  the inner one. It reads as a continuation of the jump bar rather than a new surface.

**(c) Why it serves reading rather than editing**

Orientation while scrolling is a reading need with no editing equivalent: an editor user
is usually at the caret, which a breadcrumb already describes, while a reader is usually
somewhere else on the screen. It answers the question people answer today by scrolling
back and forward, which is the most common way a reading session loses its place.

**(d) Implementation sketch**

- `internal/app/readingpin.go` (new) — the enclosing-scope computation and the row. Cache
  a sorted list of symbol ranges per (path, version) from `DocumentSymbols`; each frame,
  take `first, _ := st.VisibleLines()` (used at `editor_area.go:335`) and binary-search the
  innermost symbol whose range contains that line. That is O(log n) per frame with no
  requests and no steady-state allocation.
- **The data shape is the one thing to coordinate.** On `main`, `DocumentSymbol` carries
  only a name and a selection position, because `flattenDocumentSymbols`
  (`internal/lsp/document_symbols.go:29`) drops `kind`, the full `range` and the nesting
  depth. #40 / #56 widens it to `Name, Detail, Kind, Depth, Position, Range` (on
  `feat/ux-batch-1`); if that lands first, this candidate is a UI-only change against the
  wider struct. If it has not landed, the same three fields must be added here — and then
  whoever lands second resolves a trivial conflict. This is a dependency on shape, not a
  duplicate of the outline feature.
- `internal/app/editor_area.go` — the conditional row goes between the jump bar and
  `editor.View`; the row is not drawn at all when there is nothing to say, so the editor's
  height changes only on a scope crossing.
- `internal/config/config.go` — the toggle; `internal/app/settings.go` — its switch, in the
  Editor section near the line-height control (`settings.go:627`).
- Locales as elsewhere (one label for the row's accessible name).
- Tests: `internal/app/readingpin_test.go` on the harness — the row names the enclosing
  declaration once its line is above the viewport; it is absent at the top of the file;
  a scroll within one declaration never changes the row; nested declarations show two
  segments and an ellipsis. The computation itself (a pure function over ranges and a line)
  deserves a table test with no UI at all.

**(e) Size and work breakdown**

S–M, about 2 days: range cache and enclosing-scope computation half a day (with the table
test); the row and its layout half a day; the visibility rules and hysteresis half a day;
settings, locales and harness tests half a day.

**(f) Risks and unknowns**

- **The 22 pt shift.** Every appearance and disappearance moves the code. The hysteresis
  and the "only on a scope crossing" rule are what keep it from feeling like the window is
  breathing; if that is not enough, the follow-up is a MyGo editor top inset, which is a
  dependency change and not part of this item.
- **Signal-to-noise in nested languages.** A Go file with func literals produces scopes of
  two lines; a minimum range length (say 8 lines) before a scope is worth reporting is the
  cheap guard, and it should be in the first implementation rather than discovered later.
- **Shape collision with #56.** Described above; it is a one-file conflict, not a design
  conflict, but it must be coordinated rather than raced.
- **Servers that return flat symbols.** Some servers answer `SymbolInformation` instead of
  hierarchical symbols; those carry no nesting, so the row should say nothing rather than
  something wrong.

---

### 6. Inlay type hints

**Size: S–M.** The inferred type of a value, shown where it is declared, in the editor's own
inlay lane.

**(a) What the reader does**

You read `handler := compose(a, b)` and everything the rest of the function does depends
on what `handler` is. Opening `compose` answers it, and costs you the paragraph you were
reading. The language server knows the type; See owns the pixels; the type should be on
the line, muted, without being in the file.

**(b) UI and interaction**

- **Home: an inlay at the end of the line**, drawn by the editor inside its own text flow:
  for `handler := compose(a, b)` the line renders as code, then, after a gap, `Handler` in
  the editor's muted inlay colour, never selectable, never part of the buffer. The app
  measures and places nothing: it hands the editor `Inlay{Offset: end of the line, Text,
  Trailing: true, Ellipsis: "…"}`, and the editor draws it and moves it with the edits.
  End of line rather than after the identifier because a trailing inlay never reflows the
  code — a mid-line one pushes the rest of the line and can wrap it, which for a reader is
  worse than a hint one gap further right.
- **Which hints.** Whatever the server reports for the visible range. For Go that is
  inferred types on short variable declarations and, server settings permitting, range
  variables and parameter names; See does not decide, it draws what arrives.
- **Entry points.** Automatic, for languages whose server reports hints. Settings →
  Editor → **Type Hints**, on by default, plus the command palette entry **Toggle Type
  Hints** because a reader who finds them noisy wants one keystroke, not a settings
  dialog.
- **Scope and refresh.** Only the visible lines are requested, refreshed when scrolling
  settles (about 100 ms) and when the buffer version changes. Because the hint is a trailing
  inlay, a line with no room left shortens the hint to `…` (`Ellipsis`) instead of wrapping
  the code or drawing over the scrollbar; nothing is clipped by hand.
- **Empty, loading and error states.** Nothing is drawn: no server, no support, no hints
  for the range, a failed request. A hint that fails must be invisible, for the same reason
  as candidate 3 — the reader did not ask a question they can see, and a per-line error
  badge would be worse than the missing type.
- **Fit.** The hint has to look like a marginal note, not like text the file contains, and
  the editor's inlay style already does that: muted, drawn after the code, outside the
  buffer, not selectable, not editable. Its colour and size come from MyGo rather than from
  this candidate; a hint that must be dimmer or smaller than the editor's inlay style is a
  MyGo change, and belongs with the other upstream asks in §4.4.

**(c) Why it serves reading rather than editing**

A type that is inferred is a fact about the code that is not written in it — the single
most common reason a reader leaves a function to open another. Showing it inline, muted and
non-editable keeps the reader in place; it is also the one place where "show the reader
what the compiler knows" and "do not turn See into an editor" point the same way.

**(d) Implementation sketch**

- `internal/lsp/client.go` — `InlayHints(ctx, uri string, r Range) ([]InlayHint, error)`
  with `textDocument/inlayHint`; `protocol.go` — `InlayHint{Position, Label, Kind,
  PaddingLeft, PaddingRight}`, where `Label` is the LSP union (a string or an array of
  parts) and must be parsed tolerantly, as `ParseHover` does (`protocol.go:152`). Skip
  `inlayHint/resolve` in the first version: unresolved labels are enough for a type.
- `protocol.go:112` — `InlayHintProvider json.RawMessage` in `serverCapabilities` and
  `hasInlayHints()`; declare `"inlayHint": {}` in `initialize`'s `textDocument` block
  (`client.go:185`).
- **The request range** is the point most implementations get wrong: convert
  `st.VisibleLines()` to LSP positions with `lsp.ColumnToLSP` over the line's full text
  (`internal/lsp/position.go`, used at `app.go:1495`), so the encoding negotiation is
  honoured. Asking for the whole document instead is simpler and will make gopls slow
  enough to be noticed on large files.
- **Drawing is the editor's job, and the lane already exists.** `State.SetInlays([]Inlay)`
  (`ui/editor/state.go:478`, the type at `ui/editor/lsp.go:90`) draws text inside a line and
  keeps it attached to its offset as the buffer changes. `feat/ux-batch-1` already drives it
  for inline diagnostics and blame: `d.languageInlays []editor.Inlay` (`app.go:213`),
  merged in `editorInlays` (`internal/app/blame.go:86`) and applied with
  `st.SetInlays(a.editorInlays(d, st))` (`editor_area.go:295`). If #56 has landed, this
  candidate only fills `d.languageInlays`; if it has not, the same three lines are the whole
  app-side surface — a field on `doc`, one call after the buffer version changes, and the
  merge that keeps diagnostics and blame in the same list.
- `internal/app/inlay.go` (new) — the state only: visible range, hints by line, seq, cancel,
  scroll settle timer, and the mapping from a hint to `editor.Inlay{Offset: end of its line,
  Text: "  " + label, Trailing: true, Ellipsis: "…"}`. No measurement, no overlay, no
  clipping: a trailing inlay is elided by the editor when the line is full.
- Settings and command as above; locales; regenerate the catalog.
- Tests: `internal/lsp/inlay_test.go` on the fake server — the range sent, both label
  shapes, a `null` result; `internal/app/inlay_test.go` on the harness — `d.languageInlays`
  carries hints for visible lines only, the toggle empties it, and a stale reply for an older
  version is dropped. `blame_test.go:20` on `feat/ux-batch-1` is the harness pattern for a
  language inlay.

**(e) Size and work breakdown**

S–M, about 2 days: protocol, client, tolerant parsing and range conversion half a day; the
state, visible-range window and refresh on scroll half a day; the inlay mapping, the toggle
and its settings half a day; tests half a day. It is no longer M because the drawing half of
the work — the part that was going to fight horizontal scrolling and clipping — belongs to
the editor.

**(f) Risks and unknowns**

- **gopls's hint settings are not verified here.** See answers `workspace/configuration`
  with one null per item (`client.go:377` to `:382`, "the server's defaults"), so which
  hints gopls reports by default was not checked in this work. Some servers need a
  configuration payload before hints appear at all, which would make this candidate touch
  the client's configuration handler for every server — the one item here whose risk
  extends past its own feature.
- **The inlay lane belongs to #56, not to `main`.** On `main` there is no `SetInlays` call in
  `editor_area.go` and no `languageInlays` field, so this candidate either waits for #56 or
  adds the same three lines and resolves a trivial conflict when #56 lands. A one-file
  overlap, not a design conflict, but it must be coordinated rather than raced.
- **Hints only ever appear at the end of a line.** If reading shows that a hint after the
  identifier is worth the reflow, the inlay `Offset` already allows it; that is a design
  change to this candidate, not new plumbing.
- **Noise.** A hint on every line turns a file into a wall of grey. End-of-line only, muted
  by the editor, and only for hints the server actually reports keeps it tolerable; the
  toggle is the escape hatch.
- **Distance from the declaration.** A trailing hint sits at the line's end, so on a long
  line the eye travels further than in Xcode, which puts the hint straight after the name.
  That gap is the price of never reflowing the code; the mid-line offset above is the fix if
  it reads badly.

## 4. Why this order

### 4.1 The ranking

| # | Candidate | How often a reader needs it | What it unblocks | Cost | Why here |
| --- | --- | --- | --- | --- | --- |
| 1 | Call hierarchy panel | Every unfamiliar function | Type hierarchy, implementation trees, and the shared reading-list widget | L | The question that decides whether reading continues at all: who calls this, what does it call |
| 2 | Quick Help inspector | Every unfamiliar symbol | Navigable documentation; the home for later reading material | M | The other half of the first ten minutes, and it reuses the hover path rather than adding a request |
| 3 | Signature help | Every call site | Parameter hints, hover on arguments | M | Cheapest per use: one request, one strip, no new surface |
| 4 | Pinnable references panel | Whenever a symbol is used in more than a handful of places | A third list for free, through the widget from 1 | L | Valuable, but the popover already covers the small case |
| 5 | Reading-position pin | Long files, continuously | Nothing; it removes a recurring interruption | S–M | Pure orientation, and it depends on the symbol shape #56 introduces |
| 6 | Inlay type hints | Reading code that infers types | The editor's inlay lane, which every later language hint reuses | S–M | Last because one uncertainty is left: which hints a server reports without a configuration payload. The drawing lane is the editor's own and #56 already uses it |

The order is not an estimate of excitement; it is the sequence a reader's questions come
in: **who calls this** (1), **what does it mean** (2), **what are these arguments** (3),
**where else is it used** (4), **where am I** (5), **what type is this** (6). Each of the
first four is answered today only by leaving the code in front of you — opening another
file, or covering the one you are reading — which is the cost this application exists to
remove.

### 4.2 What each candidate unblocks

- **1** supplies the client primitives for every graph question — prepare, incoming,
  outgoing — so a type hierarchy and an implementation tree afterwards are a second slice
  of the same work, not a new project.
- **2** turns documentation into something See can navigate, and gives later material (a
  symbol's history, its commits, its annotations) a place to live that is not the sidebar.
- **3** is the first per-caret server request, and it establishes the pattern (debounce,
  cache key, dismiss state) that any later caret-driven aid copies.
- **4** exists mostly because of the shared list widget it forces out of `relatedView`:
  that widget is what makes 1 cheaper than it looks, and a third list afterwards (pinned
  problems, for instance) is then nearly free.
- **5** unblocks nothing; it removes friction from a session already underway and should
  stay as cheap as it is scoped here. If it grows past its estimate, cut it rather than let
  it delay 1 to 4.
- **6** proves the inlay lane for everything that later wants text inside a line — parameter
  names, resolved hints, a diagnostic lane — and at S–M it is cheap insurance rather than a
  dependency change.

Candidates 1 and 4 touch the same two places — the bottom panel's tab strip and the row
widget — and 2, 5 and 6 all touch `internal/app/editor_area.go`. Landing them in rank
order keeps the conflicts local: each later item rebases onto a surface the earlier one
created rather than reinventing it.

### 4.3 What was rejected

**In flight on other branches.** Each of these is already implemented or being
implemented, so no candidate above proposes it. Verified by reading the branches, not from
the pull request descriptions.

| Work | Where it lives | So this document does not propose |
| --- | --- | --- |
| Symbol outline navigator | `internal/app/outline.go`, `internal/lsp/document_symbols.go` on `feat/ux-batch-1` | a Symbols navigator or a file outline |
| Jump bar following the symbol at the cursor | `internal/app/jumpbar.go` | cursor breadcrumbs with symbol segments |
| Peek Definition | `internal/app/peek.go` | an inline definition popover |
| Go to Implementation, Go to Type Definition | `internal/app/navigation.go`, `internal/lsp/client.go` | implementation and type-definition navigation |
| Counterparts | `internal/app/counterparts.go` | header-to-implementation switching |
| Minimap | `internal/app/minimap.go` | a structure strip beside the scrollbar |
| Current-line blame, reveal in Git Log | `internal/app/blame.go`, `internal/git/blame.go` | per-line attribution |
| Line bookmarks | `internal/app/bookmarks.go`, `internal/nav/bookmarks.go` | reading bookmarks |
| Inline diagnostics at line end | `internal/app/diagnostics_inline.go` | a diagnostic lane in the editor |
| Code folding, with saved folds | `internal/app/folding.go` | folding and its persistence |
| Find and replace | `internal/app/find_replace.go`, `internal/search/replace.go` | search and replace |
| File-scoped Git log, commit row actions | `internal/app/gitlog_actions.go`, `gitlog_reveal.go` | a file's history view |
| MRU tab cycling, reopen closed tab, restore terminals | `internal/app/reopen_test.go` and the session work | tab and session recall |
| Branch comparison | PR #57, `devin/1795065831-branch-diffs` | comparing two branches |
| Window and session states | PR #37, `workspace-window-states` | window state persistence |

**Rejected on their merits.** A reader who wonders why these are absent should not have to
guess.

| Idea | Why it is not here |
| --- | --- |
| Type hierarchy (supertypes and subtypes) | Same client primitives and the same tree widget as candidate 1. It belongs immediately after it; listing it as a seventh candidate would hide that it is the second slice of the first. |
| A workspace symbol browser with kind filters | `Cmd+T` already searches workspace symbols (`internal/lsp/client.go:158`, `internal/app/quickopen.go:52`). The difference is a filter row and kind glyphs, which belongs in whatever surface the outline work leaves behind, not in a new one. |
| Model-assisted "explain this code" | No model client exists in the repository, and a reading aid that needs a network, a key and a privacy story is a different product argument. It should not be smuggled in through a spec like this one. |
| Complexity metrics per declaration (SciTools) | Needs a parser per language that See does not have and that gopls does not expose. Useful for deciding what to read in an unfamiliar repository; useless inside a file you have already opened. |
| A workspace dependency or architecture graph | The language server protocol gives per-symbol facts, not a module graph, and no server See starts answers such a request. Large, speculative, and the least certain of everything considered. |
| Semantic highlighting | MyGo's editor draws tokens from See's own tables (`internal/app/syntax.go`); semantic token classes need editor support that does not exist. A dependency change wearing a feature's clothes. |
| Peek the whole file, or a fuzzy file peek | No reading question that Open Quickly (`internal/app/quickopen.go:18`) does not already answer. |
| A third navigation memory (a reading trail) | Back and forward already exist (`internal/nav/history.go`) and #56 adds bookmarks. A third memory competes with both. |
| Pinning diagnostics | It could ride candidate 4's widget later. Not a reading question of its own; the Problems navigator already filters and jumps. |
| A "quick help" that only reformats the hover card | Cosmetic. The value is persistence and following, which is candidate 2, not a bigger popover. |

### 4.4 Decisions the owner has to make

1. **Does the bottom panel become the reading surface (candidates 1 and 4), or do these
   lists belong in the sidebar?** Recommendation: the panel. The sidebar is for navigating
   the workspace; a tree of calls is not a place to go, it is a thing to read.
2. **Is the Quick Help inspector per window or per editor group?** Recommendation: per
   window, on the right edge, following the focused group's caret — a group of its own
   would compete with the split-layout feature for the same space.
3. **Should See answer `workspace/configuration` with real per-server settings, or keep
   answering null and take the servers' defaults?** (`internal/lsp/client.go:377`,
   the handler at `:375` to `:390`.) Candidate 6 may need a real answer for gopls to report
   hints at all, and that is a change to every server, not one feature. This is the only
   item in the document with risk outside its own scope; decide it before starting
   candidate 6.
4. **Do we need further MyGo editor APIs — a caret callback (candidate 3), a top inset
   (candidate 5) — or can every remaining surface stay app-side?** Inlay rendering is no
   longer part of the question: `SetInlays` exists (`ui/editor/state.go:478`) and #56 already
   drives it. Recommendation: app-side for both, as scoped above; upstream the caret callback
   only once candidate 3 has shown what it needs from it.

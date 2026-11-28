package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Position is a place in a document: a line, from 0, and a character
// offset in the negotiated encoding.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span of a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// DocumentHighlight is an occurrence of a symbol in the current document.
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

// Location is a range of a document named by its URI.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type FileChangeType int

const (
	FileCreated FileChangeType = iota + 1
	FileChanged
	FileDeleted
)

type FileChange struct {
	URI  string         `json:"uri"`
	Type FileChangeType `json:"type"`
}

type Location struct {
	Name  string `json:"-"`
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// DocumentLink is a span of a document that is a link, as an import
// path or a URL in a comment: where Target is empty, documentLink/resolve
// fills it, with Data the server's own handle to the link.
type DocumentLink struct {
	Range   Range           `json:"range"`
	Target  string          `json:"target,omitempty"`
	Tooltip string          `json:"tooltip,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// CallItem is a call hierarchy item: a symbol calls reach or leave,
// with the data a hierarchy item carries, which the server wants back
// when its calls are asked for.
type CallItem struct {
	ID             int             `json:"-"`
	Name           string          `json:"name"`
	Kind           int             `json:"kind"`
	Detail         string          `json:"detail,omitempty"`
	URI            string          `json:"uri"`
	Range          Range           `json:"range"`
	SelectionRange Range           `json:"selectionRange"`
	Data           json.RawMessage `json:"data,omitempty"`
	Tags           []int           `json:"tags,omitempty"`
}

type TypeHierarchyItem CallItem

// CallEdge is one answer to incomingCalls or outgoingCalls: an item on
// the other side of the direction, and the ranges the call sites cover
// on the caller's side (fromRanges for both requests).
type CallEdge struct {
	Item       CallItem
	FromRanges []Range
}

// A hierarchy item keeps what the protocol carries back and forth: the
// data field servers give to prepare answers and want returned verbatim.
type callItemWire struct {
	Name           string          `json:"name"`
	Kind           int             `json:"kind"`
	Detail         string          `json:"detail,omitempty"`
	URI            string          `json:"uri"`
	Range          Range           `json:"range"`
	SelectionRange Range           `json:"selectionRange"`
	Data           json.RawMessage `json:"data,omitempty"`
}

type locationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// ParseLocations decodes the result of textDocument/definition and its
// kin: null, a Location, Location[] or LocationLink[]. Links become the
// Location of their target's selection range, the name rather than the
// whole declaration.
func ParseLocations(raw json.RawMessage) ([]Location, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	switch raw[0] {
	case '{':
		loc, err := parseOne(raw)
		if err != nil {
			return nil, err
		}
		return []Location{loc}, nil
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("lsp: bad locations: %w", err)
		}
		out := make([]Location, 0, len(items))
		for _, it := range items {
			loc, err := parseOne(it)
			if err != nil {
				return nil, err
			}
			out = append(out, loc)
		}
		return out, nil
	}
	return nil, fmt.Errorf("lsp: bad locations: unexpected %.20q", raw)
}

func parseOne(raw json.RawMessage) (Location, error) {
	var probe struct {
		URI       *string `json:"uri"`
		TargetURI *string `json:"targetUri"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Location{}, fmt.Errorf("lsp: bad location: %w", err)
	}
	switch {
	case probe.TargetURI != nil:
		var l locationLink
		if err := json.Unmarshal(raw, &l); err != nil {
			return Location{}, fmt.Errorf("lsp: bad location link: %w", err)
		}
		return Location{URI: l.TargetURI, Range: l.TargetSelectionRange}, nil
	case probe.URI != nil:
		var l Location
		if err := json.Unmarshal(raw, &l); err != nil {
			return Location{}, fmt.Errorf("lsp: bad location: %w", err)
		}
		return l, nil
	}
	return Location{}, fmt.Errorf("lsp: location without uri or targetUri")
}

// Text document sync kinds.
const (
	SyncNone        = 0
	SyncFull        = 1
	SyncIncremental = 2
)

// serverCapabilities holds the parts of the server's capabilities the
// client reads.
type serverCapabilities struct {
	HoverProvider           json.RawMessage `json:"hoverProvider"`
	ReferencesProvider      json.RawMessage `json:"referencesProvider"`
	DocumentSymbolProvider  json.RawMessage `json:"documentSymbolProvider"`
	WorkspaceSymbolProvider json.RawMessage `json:"workspaceSymbolProvider"`

	PositionEncoding          json.RawMessage `json:"positionEncoding"`
	TextDocumentSync          json.RawMessage `json:"textDocumentSync"`
	DefinitionProvider        json.RawMessage `json:"definitionProvider"`
	DocumentHighlightProvider json.RawMessage `json:"documentHighlightProvider"`
	ImplementationProvider    json.RawMessage `json:"implementationProvider"`
	TypeDefinitionProvider    json.RawMessage `json:"typeDefinitionProvider"`
	CallHierarchyProvider     json.RawMessage `json:"callHierarchyProvider"`
	TypeHierarchyProvider     json.RawMessage `json:"typeHierarchyProvider"`
	DiagnosticProvider        json.RawMessage `json:"diagnosticProvider"`
	InlayHintProvider         json.RawMessage `json:"inlayHintProvider"`
	DocumentLinkProvider      json.RawMessage `json:"documentLinkProvider"`
	SignatureHelpProvider     json.RawMessage `json:"signatureHelpProvider"`
	CompletionProvider        json.RawMessage `json:"completionProvider"`
	SemanticTokensProvider    json.RawMessage `json:"semanticTokensProvider"`
	FoldingRangeProvider      json.RawMessage `json:"foldingRangeProvider"`
	CodeActionProvider        json.RawMessage `json:"codeActionProvider"`
	ExecuteCommandProvider    json.RawMessage `json:"executeCommandProvider"`
	SelectionRangeProvider    json.RawMessage `json:"selectionRangeProvider"`
	RenameProvider            json.RawMessage `json:"renameProvider"`
	DocumentFormatting        json.RawMessage `json:"documentFormattingProvider"`
	DocumentRangeFormatting   json.RawMessage `json:"documentRangeFormattingProvider"`
}

// syncKind reads textDocumentSync, a number or TextDocumentSyncOptions,
// and whether the server wants didOpen and didClose.
func (s serverCapabilities) syncKind() (kind int, openClose bool) {
	raw := bytes.TrimSpace(s.TextDocumentSync)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return SyncNone, false
	}
	if raw[0] == '{' {
		var opts struct {
			OpenClose bool `json:"openClose"`
			Change    int  `json:"change"`
		}
		if json.Unmarshal(raw, &opts) != nil {
			return SyncNone, false
		}
		return opts.Change, opts.OpenClose
	}
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return SyncNone, false
	}
	// A bare kind implies open and close notifications.
	return n, n != SyncNone
}

func (s serverCapabilities) hasDefinition() bool {
	raw := bytes.TrimSpace(s.DefinitionProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasDocumentHighlights() bool {
	raw := bytes.TrimSpace(s.DocumentHighlightProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasInlayHints() bool {
	raw := bytes.TrimSpace(s.InlayHintProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasDocumentLinks() bool {
	raw := bytes.TrimSpace(s.DocumentLinkProvider)
	return providerEnabled(raw)
}

// documentLinkResolve reports whether the server fills a link's target
// on documentLink/resolve rather than in the documentLink answer.
func (s serverCapabilities) documentLinkResolve() bool {
	raw := bytes.TrimSpace(s.DocumentLinkProvider)
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var opts struct {
		ResolveProvider bool `json:"resolveProvider"`
	}
	return json.Unmarshal(raw, &opts) == nil && opts.ResolveProvider
}

// InlayHint is a type or parameter name the server would draw inside a
// line: at Position, the Label's text, padded as it asks.
type InlayHint struct {
	Position     Position
	Label        string
	Kind         int
	PaddingLeft  bool
	PaddingRight bool
}

type inlayHintJSON struct {
	Position     Position        `json:"position"`
	Label        json.RawMessage `json:"label"`
	Kind         int             `json:"kind"`
	PaddingLeft  bool            `json:"paddingLeft"`
	PaddingRight bool            `json:"paddingRight"`
}

// ParseInlayHints decodes a textDocument/inlayHint result: null or a
// list, each hint's label a string or an array of parts, as the union
// allows; inlayHint/resolve is not asked, so unresolved labels — the
// parts — are enough.
func ParseInlayHints(raw json.RawMessage) ([]InlayHint, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var items []inlayHintJSON
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("lsp: bad inlay hints: %w", err)
	}
	out := make([]InlayHint, 0, len(items))
	for _, it := range items {
		label, err := inlayLabel(bytes.TrimSpace(it.Label))
		if err != nil {
			return nil, err
		}
		out = append(out, InlayHint{Position: it.Position, Label: label, Kind: it.Kind, PaddingLeft: it.PaddingLeft, PaddingRight: it.PaddingRight})
	}
	return out, nil
}

// SelectionRange is one answer of textDocument/selectionRange: the
// range a position's selection grows to, linked to the ranges it grows
// through after it, innermost first.
type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}

// ParseSelectionRanges decodes a textDocument/selectionRange result:
// null or a list of nested SelectionRange.
func ParseSelectionRanges(raw json.RawMessage) ([]*SelectionRange, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var ranges []*SelectionRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, fmt.Errorf("lsp: bad selection ranges: %w", err)
	}
	return ranges, nil
}

// ParseDocumentLinks decodes a textDocument/documentLink result: null
// or a list of DocumentLink.
func ParseDocumentLinks(raw json.RawMessage) ([]DocumentLink, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var links []DocumentLink
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, fmt.Errorf("lsp: bad document links: %w", err)
	}
	return links, nil
}

func inlayLabel(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	switch raw[0] {
	case '"':
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err
	case '[':
		var parts []struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(raw, &parts); err != nil {
			return "", fmt.Errorf("lsp: bad inlay hint label: %w", err)
		}
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.Value)
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("lsp: bad inlay hint label %.20q", raw)
}

// semanticTokensLegend is the legend a semanticTokensProvider names:
// the token type and modifier names its numeric stream indexes.
type semanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// SemanticTokensData is the result of a semanticTokens request: the
// delta-encoded token stream, five numbers a token.
type SemanticTokensData struct {
	Data []uint32 `json:"data"`
}

// semanticTokens reads the provider's legend and whether it answers
// full and range requests. The provider may be an object or, against
// the spec, a bare true; a legend it does not give leaves no names, and
// its requests then count for nothing.
func (s serverCapabilities) semanticTokens() (legend semanticTokensLegend, full, rng bool) {
	raw := bytes.TrimSpace(s.SemanticTokensProvider)
	if !providerEnabled(raw) {
		return legend, false, false
	}
	if raw[0] != '{' {
		return legend, false, false
	}
	var opts struct {
		Legend semanticTokensLegend `json:"legend"`
		Full   json.RawMessage      `json:"full"`
		Range  json.RawMessage      `json:"range"`
	}
	if json.Unmarshal(raw, &opts) != nil || len(opts.Legend.TokenTypes) == 0 {
		return legend, false, false
	}
	truthy := func(v json.RawMessage) bool {
		v = bytes.TrimSpace(v)
		return providerEnabled(v)
	}
	return opts.Legend, truthy(opts.Full), truthy(opts.Range)
}

func (s serverCapabilities) hasFoldingRange() bool {
	raw := bytes.TrimSpace(s.FoldingRangeProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasSignatureHelp() bool {
	raw := bytes.TrimSpace(s.SignatureHelpProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasCompletion() bool {
	raw := bytes.TrimSpace(s.CompletionProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) completionItemResolve() bool {
	raw := bytes.TrimSpace(s.CompletionProvider)
	if !s.hasCompletion() || len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var opts struct {
		ResolveProvider bool `json:"resolveProvider"`
	}
	return json.Unmarshal(raw, &opts) == nil && opts.ResolveProvider
}

func (s serverCapabilities) hasCallHierarchy() bool {
	raw := bytes.TrimSpace(s.CallHierarchyProvider)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasSelectionRanges() bool {
	raw := bytes.TrimSpace(s.SelectionRangeProvider)
	return providerEnabled(raw)
}

// codeActionSupport is what a server says about textDocument/codeAction
// in codeActionProvider: a literal for the kinds it gives and whether
// codeAction/resolve fills an action in, or a plain true.
type codeActionSupport struct {
	resolve bool
	kinds   []string
}

func (s serverCapabilities) codeActions() (codeActionSupport, bool) {
	raw := bytes.TrimSpace(s.CodeActionProvider)
	if !providerEnabled(raw) {
		return codeActionSupport{}, false
	}
	if !bytes.Equal(raw, []byte("true")) {
		var opts struct {
			CodeActionKinds []string `json:"codeActionKinds"`
			ResolveProvider bool     `json:"resolveProvider"`
		}
		if json.Unmarshal(raw, &opts) == nil {
			return codeActionSupport{resolve: opts.ResolveProvider, kinds: opts.CodeActionKinds}, true
		}
	}
	if !bytes.Equal(raw, []byte("true")) {
		return codeActionSupport{}, false
	}
	return codeActionSupport{}, true
}

// hasRename reads renameProvider: a boolean, or a literal whose
// prepareProvider tells whether the server answers
// textDocument/prepareRename before textDocument/rename.
func (s serverCapabilities) hasRename() (rename, prepare bool) {
	raw := bytes.TrimSpace(s.RenameProvider)
	if !providerEnabled(raw) {
		return false, false
	}
	if !bytes.Equal(raw, []byte("true")) {
		var opts struct {
			PrepareProvider bool `json:"prepareProvider"`
		}
		if json.Unmarshal(raw, &opts) == nil {
			return true, opts.PrepareProvider
		}
	}
	return bytes.Equal(raw, []byte("true")), false
}

// FormattingOptions are the indentation settings a formatting request
// carries, as the file's own indenting does.
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

func (s serverCapabilities) hasDocumentFormatting() bool {
	raw := bytes.TrimSpace(s.DocumentFormatting)
	return providerEnabled(raw)
}

func (s serverCapabilities) hasDocumentRangeFormatting() bool {
	raw := bytes.TrimSpace(s.DocumentRangeFormatting)
	return providerEnabled(raw)
}

// ParseTextEdits decodes the result of a formatting request: null or a
// list of TextEdit.
func ParseTextEdits(raw json.RawMessage) ([]TextEdit, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var edits []TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, fmt.Errorf("lsp: bad text edits: %w", err)
	}
	return edits, nil
}

// Command is a server's named operation with its arguments, run in the
// server process: workspace/executeCommand sends it back as it came.
type Command struct {
	Title     string          `json:"title"`
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// TextEdit replaces Range with NewText: the documentChanges of a
// workspace edit are lists of these.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit is the edit of a code action or a workspace/applyEdit
// request: per document, a list of TextEdits. It may arrive as the
// legacy changes map or as documentChanges.
type WorkspaceEdit struct {
	// Changes maps a document URI to its edits.
	Changes map[string][]TextEdit `json:"changes,omitempty"`
	// DocumentChanges holds the `documentChanges` form. An entry is a
	// TextDocumentEdit or a resource operation (create, rename, delete).
	DocumentChanges []json.RawMessage `json:"documentChanges,omitempty"`
}

// TextDocumentEdit carries the edits of one document inside
// documentChanges.
type TextDocumentEdit struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version *int64 `json:"version"`
	} `json:"textDocument"`
	Edits []json.RawMessage `json:"edits"`
}

// EditURIs is the document URIs a workspace edit touches, changes and
// text-document documentChanges together, each once. Entries the client
// cannot read are left out.
func (w *WorkspaceEdit) EditURIs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	for u := range w.Changes {
		add(u)
	}
	for _, raw := range w.DocumentChanges {
		var edit TextDocumentEdit
		if json.Unmarshal(raw, &edit) == nil {
			add(edit.TextDocument.URI)
		}
	}
	return out
}

// EditsFor is the TextEdits of one document URI from both forms. Edits
// whose shape the client cannot read — AnnotatedTextEdits inside
// documentChanges, which the client never advertises — are left out.
func (w *WorkspaceEdit) EditsFor(docURI string) []TextEdit {
	var out []TextEdit
	out = append(out, w.Changes[docURI]...)
	for _, raw := range w.DocumentChanges {
		var edit TextDocumentEdit
		if json.Unmarshal(raw, &edit) != nil || edit.TextDocument.URI != docURI {
			continue
		}
		for _, rawEdit := range edit.Edits {
			var e TextEdit
			if json.Unmarshal(rawEdit, &e) == nil {
				out = append(out, e)
			}
		}
	}
	return out
}

// ResourceChanges is the number of documentChanges entries that are
// resource operations rather than text edits: the app cannot run them.
func (w *WorkspaceEdit) ResourceChanges() int {
	n := 0
	for _, raw := range w.DocumentChanges {
		var probe struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(raw, &probe) == nil && (probe.Kind == "create" || probe.Kind == "rename" || probe.Kind == "delete") {
			n++
		}
	}
	return n
}

// CodeAction is a fix or refactoring a server offers at a place. A
// Command alone may arrive too (Command | CodeAction): UnmarshalJSON
// folds it into one with Command set. An action with neither Edit nor
// Command needs codeAction/resolve before it runs.
type CodeAction struct {
	Title       string
	Kind        string
	Edit        *WorkspaceEdit
	Command     *Command
	Data        json.RawMessage
	IsPreferred bool
	Disabled    string // the reason an unavailable action gives
}

func (a *CodeAction) UnmarshalJSON(data []byte) error {
	var raw struct {
		Title    string          `json:"title"`
		Kind     string          `json:"kind"`
		Edit     *WorkspaceEdit  `json:"edit"`
		Command  json.RawMessage `json:"command"`
		Data     json.RawMessage `json:"data"`
		Disabled *struct {
			Reason string `json:"reason"`
		} `json:"disabled"`
		IsPreferred bool `json:"isPreferred"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	a.Title, a.Kind, a.Edit, a.Data, a.IsPreferred = raw.Title, raw.Kind, raw.Edit, raw.Data, raw.IsPreferred
	a.Disabled, a.Command = "", nil
	if raw.Disabled != nil {
		a.Disabled = raw.Disabled.Reason
	}
	if len(bytes.TrimSpace(raw.Command)) == 0 || bytes.Equal(bytes.TrimSpace(raw.Command), []byte("null")) {
		return nil
	}
	// In a Command-shaped reply `command` is the command string; in a
	// CodeAction it is a Command object.
	if raw.Command[0] == '"' {
		var name string
		if err := json.Unmarshal(raw.Command, &name); err != nil {
			return err
		}
		var args json.RawMessage
		var probe struct {
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(data, &probe) == nil {
			args = probe.Arguments
		}
		a.Command = &Command{Title: a.Title, Command: name, Arguments: args}
		return nil
	}
	var cmd Command
	if err := json.Unmarshal(raw.Command, &cmd); err != nil {
		return err
	}
	a.Command = &cmd
	return nil
}

// MarshalJSON writes an action back as the server sent it, so
// codeAction/resolve gets the untouched object the spec asks for.
func (a CodeAction) MarshalJSON() ([]byte, error) {
	raw := struct {
		Title       string          `json:"title"`
		Kind        string          `json:"kind,omitempty"`
		Edit        *WorkspaceEdit  `json:"edit,omitempty"`
		Command     *Command        `json:"command,omitempty"`
		Data        json.RawMessage `json:"data,omitempty"`
		IsPreferred bool            `json:"isPreferred,omitempty"`
	}{a.Title, a.Kind, a.Edit, a.Command, a.Data, a.IsPreferred}
	return json.Marshal(raw)
}

// NeedsResolve reports whether the action must round-trip
// codeAction/resolve before it can run: nothing to run yet.
func (a *CodeAction) NeedsResolve() bool {
	return a.Edit == nil && a.Command == nil
}

// PrepareRenameResult is what textDocument/prepareRename answers: a
// plain Range, a {range, placeholder} literal, or {defaultBehavior}.
type PrepareRenameResult struct {
	Range           Range
	Placeholder     string // the name to seed the prompt with
	DefaultBehavior bool   // the server wants a rename without validation
}

// Empty reports whether the result holds no range and no default
// behavior, as a {placeholder} alone or a null server sent.
func (r *PrepareRenameResult) Empty() bool {
	return r == nil || (!r.DefaultBehavior && r.Range == Range{})
}

// ParsePrepareRename decodes a prepareRename result into
// PrepareRenameResult. A null or empty result means the position
// cannot be renamed.
func ParsePrepareRename(raw json.RawMessage) (*PrepareRenameResult, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var v struct {
		Range           *Range `json:"range"`
		Placeholder     string `json:"placeholder"`
		DefaultBehavior bool   `json:"defaultBehavior"`
		Start           *struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
		End *struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"end"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("lsp: bad prepareRename result: %w", err)
	}
	switch {
	case v.Range != nil:
		return &PrepareRenameResult{Range: *v.Range, Placeholder: v.Placeholder}, nil
	case v.Start != nil && v.End != nil:
		r := Range{
			Start: Position{Line: v.Start.Line, Character: v.Start.Character},
			End:   Position{Line: v.End.Line, Character: v.End.Character},
		}
		return &PrepareRenameResult{Range: r}, nil
	case v.DefaultBehavior:
		return &PrepareRenameResult{DefaultBehavior: true}, nil
	}
	return nil, nil
}

// ParseHover decodes a Hover result into Markdown: its contents are
// MarkupContent, a MarkedString or MarkedString[]; plain text is fenced
// as text so that it shows as written.
func ParseHover(raw json.RawMessage) (string, *Range, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil, nil
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return "", nil, fmt.Errorf("lsp: bad hover: %w", err)
	}
	md, err := markedToMarkdown(bytes.TrimSpace(h.Contents))
	return md, h.Range, err
}

func markedToMarkdown(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	switch raw[0] {
	case '"':
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", fmt.Errorf("lsp: bad hover contents: %w", err)
		}
		var parts []string
		for _, it := range items {
			s, err := markedToMarkdown(bytes.TrimSpace(it))
			if err != nil {
				return "", err
			}
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n\n---\n\n"), nil
	case '{':
		var m struct {
			Kind     string `json:"kind"`
			Language string `json:"language"`
			Value    string `json:"value"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return "", fmt.Errorf("lsp: bad hover contents: %w", err)
		}
		switch {
		case m.Language != "": // a MarkedString of code
			return "```" + m.Language + "\n" + m.Value + "\n```", nil
		case m.Kind == "plaintext":
			return "```text\n" + m.Value + "\n```", nil
		}
		return m.Value, nil
	}
	return "", fmt.Errorf("lsp: bad hover contents %.20q", raw)
}

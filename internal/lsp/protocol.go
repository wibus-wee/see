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

// Location is a range of a document named by its URI.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type Location struct {
	Name  string `json:"-"`
	URI   string `json:"uri"`
	Range Range  `json:"range"`
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
	PositionEncoding   string          `json:"positionEncoding"`
	TextDocumentSync   json.RawMessage `json:"textDocumentSync"`
	DefinitionProvider json.RawMessage `json:"definitionProvider"`
	DiagnosticProvider json.RawMessage `json:"diagnosticProvider"`
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
	return len(raw) > 0 && !bytes.Equal(raw, []byte("false")) && !bytes.Equal(raw, []byte("null"))
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

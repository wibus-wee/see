package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// SignatureHelp is the answer to textDocument/signatureHelp: the
// signatures of the call the caret is inside, with the signature and
// parameter the server marks active.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature *int                   `json:"activeSignature"`
	ActiveParameter *int                   `json:"activeParameter"`
}

// SignatureInformation describes one callable signature: its label as
// the server wrote it, its documentation, and its parameters.
type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation string                 `json:"-"`
	Parameters    []ParameterInformation `json:"parameters"`
}

// ParameterInformation describes one parameter of a signature. Its
// Label is either a substring of the signature's label or a
// [start, end] offset pair into it, which ParamStart and ParamEnd
// normalize.
type ParameterInformation struct {
	Label         parameterLabel `json:"label"`
	Documentation string         `json:"-"`
}

// parameterLabel keeps the raw form of a parameter label: a string, or
// offsets into the signature's label.
type parameterLabel struct {
	text       string
	start, end int
	offsets    bool
}

func (l *parameterLabel) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	switch raw[0] {
	case '"':
		l.offsets = false
		return json.Unmarshal(raw, &l.text)
	case '[':
		var pair []int
		if err := json.Unmarshal(raw, &pair); err != nil {
			return err
		}
		if len(pair) != 2 {
			return fmt.Errorf("lsp: bad parameter label offsets: %s", raw)
		}
		l.offsets, l.start, l.end = true, pair[0], pair[1]
		return nil
	}
	return fmt.Errorf("lsp: bad parameter label: %s", raw)
}

// ParamBounds returns where the parameter's label sits inside the
// signature's label: the label itself where the server named a
// substring, the offsets where it gave them, or (-1, -1) where the
// label does not appear in the signature at all.
func (p ParameterInformation) ParamBounds(label string) (int, int) {
	if p.Label.offsets {
		return p.Label.start, p.Label.end
	}
	if i := strings.Index(label, p.Label.text); i >= 0 {
		return i, i + len(p.Label.text)
	}
	return -1, -1
}

// ParseSignatureHelp decodes a textDocument/signatureHelp result: null
// and an object without signatures both mean nothing to show.
func ParseSignatureHelp(raw json.RawMessage) (*SignatureHelp, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var h struct {
		Signatures []struct {
			Label         string          `json:"label"`
			Documentation json.RawMessage `json:"documentation"`
			Parameters    []struct {
				Label         parameterLabel  `json:"label"`
				Documentation json.RawMessage `json:"documentation"`
			} `json:"parameters"`
		} `json:"signatures"`
		ActiveSignature *int `json:"activeSignature"`
		ActiveParameter *int `json:"activeParameter"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("lsp: bad signature help: %w", err)
	}
	help := &SignatureHelp{ActiveSignature: h.ActiveSignature, ActiveParameter: h.ActiveParameter}
	for _, s := range h.Signatures {
		sig := SignatureInformation{Label: s.Label}
		doc, err := markupToText(s.Documentation)
		if err != nil {
			return nil, err
		}
		sig.Documentation = doc
		for _, p := range s.Parameters {
			pd, err := markupToText(p.Documentation)
			if err != nil {
				return nil, err
			}
			sig.Parameters = append(sig.Parameters, ParameterInformation{Label: p.Label, Documentation: pd})
		}
		help.Signatures = append(help.Signatures, sig)
	}
	return help, nil
}

// markupToText renders documentation of the LSP MarkupContent or plain
// string forms; markdown passes through.
func markupToText(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	if raw[0] == '"' {
		var s string
		return s, json.Unmarshal(raw, &s)
	}
	var m struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("lsp: bad markup: %w", err)
	}
	return m.Value, nil
}

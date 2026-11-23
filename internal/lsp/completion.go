package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type CompletionTextEdit struct {
	Range         *Range
	NewText       string
	InsertReplace bool
}

type CompletionItem struct {
	Label              string
	Kind               int
	Detail             string
	LabelDetails       CompletionItemLabelDetails
	Documentation      string
	FilterText         string
	InsertText         string
	InsertTextFormat   int
	TextEdit           *CompletionTextEdit
	HasAdditionalEdits bool
	HasCommand         bool
	Raw                json.RawMessage
}

type CompletionItemLabelDetails struct {
	Detail      string `json:"detail"`
	Description string `json:"description"`
}

func (item CompletionItem) DisplayDetail() string {
	if item.Detail != "" {
		return item.Detail
	}
	if item.LabelDetails.Detail != "" {
		return item.LabelDetails.Detail
	}
	return item.LabelDetails.Description
}

type CompletionResult struct {
	IsIncomplete bool
	Items        []CompletionItem
}

// ParseCompletion accepts the array and CompletionList forms of a
// textDocument/completion result; null means that there are no items.
func ParseCompletion(raw json.RawMessage) (*CompletionResult, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	result := &CompletionResult{}
	var items []json.RawMessage
	switch raw[0] {
	case '[':
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("lsp: bad completion list: %w", err)
		}
	case '{':
		var list struct {
			IsIncomplete bool              `json:"isIncomplete"`
			Items        []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("lsp: bad completion list: %w", err)
		}
		result.IsIncomplete = list.IsIncomplete
		items = list.Items
	default:
		return nil, fmt.Errorf("lsp: bad completion result")
	}
	for _, rawItem := range items {
		item, err := parseCompletionItem(rawItem)
		if err != nil {
			return nil, err
		}
		if item == nil {
			continue
		}
		result.Items = append(result.Items, *item)
	}
	return result, nil
}

func ParseCompletionItem(raw json.RawMessage) (*CompletionItem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] != '{' {
		return nil, fmt.Errorf("lsp: bad completion item")
	}
	return parseCompletionItem(raw)
}

func parseCompletionItem(raw json.RawMessage) (*CompletionItem, error) {
	var wire struct {
		Label               string                     `json:"label"`
		Kind                int                        `json:"kind"`
		Detail              string                     `json:"detail"`
		LabelDetails        CompletionItemLabelDetails `json:"labelDetails"`
		Documentation       json.RawMessage            `json:"documentation"`
		FilterText          string                     `json:"filterText"`
		InsertText          string                     `json:"insertText"`
		InsertTextFormat    int                        `json:"insertTextFormat"`
		TextEdit            json.RawMessage            `json:"textEdit"`
		AdditionalTextEdits []json.RawMessage          `json:"additionalTextEdits"`
		Command             json.RawMessage            `json:"command"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("lsp: bad completion item: %w", err)
	}
	item := &CompletionItem{
		Label: wire.Label, Kind: wire.Kind, Detail: wire.Detail,
		LabelDetails: wire.LabelDetails, FilterText: wire.FilterText,
		InsertText: wire.InsertText, InsertTextFormat: wire.InsertTextFormat,
		HasAdditionalEdits: len(wire.AdditionalTextEdits) > 0,
		HasCommand:         len(wire.Command) > 0 && !bytes.Equal(bytes.TrimSpace(wire.Command), []byte("null")),
		Raw:                append(json.RawMessage(nil), raw...),
	}
	var err error
	item.Documentation, err = markupToText(wire.Documentation)
	if err != nil {
		return nil, err
	}
	if len(wire.TextEdit) > 0 && !bytes.Equal(bytes.TrimSpace(wire.TextEdit), []byte("null")) {
		var edit struct {
			Range   *Range `json:"range"`
			NewText string `json:"newText"`
			Insert  *Range `json:"insert"`
			Replace *Range `json:"replace"`
		}
		if err := json.Unmarshal(wire.TextEdit, &edit); err != nil {
			return nil, fmt.Errorf("lsp: bad completion text edit: %w", err)
		}
		item.TextEdit = &CompletionTextEdit{Range: edit.Range, NewText: edit.NewText, InsertReplace: edit.Range == nil && (edit.Insert != nil || edit.Replace != nil)}
	}
	return item, nil
}

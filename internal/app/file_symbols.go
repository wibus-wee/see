package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"see/internal/lsp"
	"see/internal/search"
	"see/locales"
)

func (a *App) filterFileSymbols() {
	q := &a.quick
	query := strings.ToLower(strings.TrimSpace(q.query[1:]))
	q.symbols = nil
	for _, symbol := range q.fileSymbols {
		if strings.Contains(strings.ToLower(symbol.name), query) {
			q.symbols = append(q.symbols, symbol)
		}
	}
	q.results = make([]search.Scored, len(q.symbols))
}

func (a *App) searchFileSymbols() {
	q := &a.quick
	d := a.docs[q.filePath]
	m := a.serverFor(q.filePath)
	if d == nil || d.buf == nil || m == nil {
		q.symbolErr = errors.New(locales.MsgNoLanguageServerIsConfiguredForThisFile())
		return
	}
	snap, docURI, path := d.buf.Snapshot(), d.uri, d.path
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	q.symbolCancel, q.symbolLoading = cancel, true
	gen, request, query := a.gen, q.gen, q.query
	unsupported := locales.Catalog.Format(i18n.Locale(), "fileSymbolsUnsupported", nil)
	go func() {
		defer cancel()
		client, err := m.Get(ctx)
		var symbols []quickSymbol
		if err == nil {
			err = m.Sync(ctx, client, docURI, snap)
		}
		if err == nil {
			if provider, ok := client.(interface {
				DocumentSymbols(context.Context, string) ([]lsp.DocumentSymbol, error)
			}); ok {
				var result []lsp.DocumentSymbol
				result, err = provider.DocumentSymbols(ctx, docURI)
				for _, symbol := range result {
					symbols = append(symbols, quickSymbol{name: symbol.Name, enc: client.Encoding(), target: target{path: path, pos: symbol.Position}})
				}
			} else {
				err = errors.New(unsupported)
			}
		}
		a.post.Post(func() {
			if a.gen != gen || q.gen != request || !q.open || q.query != query || a.activePath() != path || a.docs[path] != d || d.buf.Version() != snap.Version() || a.serverFor(path) != m {
				return
			}
			q.symbolCancel, q.symbolLoading, q.symbolErr = nil, false, err
			q.fileLoaded, q.fileSymbols = err == nil, symbols
			a.filterFileSymbols()
		})
	}()
}

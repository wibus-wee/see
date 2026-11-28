package app

import (
	"see/internal/langserver"
	"slices"
)

func (a *App) removeTrust(path string) {
	a.cfg.Trusted = slices.DeleteFunc(a.cfg.Trusted, func(t string) bool { return t == path })
	a.settingsChanged()
	if a.trustChanged != nil {
		a.trustChanged()
	} else {
		a.enforceTrust()
	}
}

func (a *App) enforceTrust() {
	if a.root == "" || a.workspaceTrusted() {
		return
	}
	a.stopLanguageServers()
	a.initLanguageServers()
	a.cancelNav()
	a.invalidateHover()
	a.afterTrust = nil
	a.trustDismissed = false
	a.trustAsk = true
	for _, d := range a.docs {
		d.diagnostics = nil
		for _, g := range a.groups() {
			if st := g.eds[d.path]; st != nil && d.buf != nil {
				st.SetDiagnostics(d.buf.Version(), nil)
			}
		}
	}
}

func (a *App) stopLanguageServers() {
	a.serverEpoch++
	a.tsEpoch++
	for _, m := range []*langserver.Server{a.lsp, a.tsLSP, a.clangLSP, a.cmakeLSP} {
		if m != nil {
			m.Stop()
		}
	}
	for _, m := range a.customLSPs {
		m.Stop()
	}
	a.lsp, a.tsLSP, a.clangLSP, a.cmakeLSP = nil, nil, nil, nil
	a.lspState, a.lspErr = langserver.Off, nil
	a.tsState, a.tsErr = langserver.Off, nil
	a.clangState, a.clangErr = langserver.Off, nil
	a.cmakeState, a.cmakeErr = langserver.Off, nil
	a.customLSPs, a.customLSPSpecs, a.customCovers, a.customStates = nil, nil, nil, nil
}

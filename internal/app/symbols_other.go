//go:build !darwin

package app

func loadSystemSymbol(string) *symbolImage { return &symbolImage{} }

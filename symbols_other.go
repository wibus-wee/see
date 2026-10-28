//go:build !darwin

package main

func loadSystemSymbol(string) *symbolImage { return &symbolImage{} }

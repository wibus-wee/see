//go:build !darwin

package app

import "testing"

func runTestsWithAppKitMainThread(m *testing.M) int { return m.Run() }

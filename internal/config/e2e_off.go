//go:build !e2e

package config

// E2EBuild reports a browser-test build (go build -tags e2e). Release builds are never one.
const E2EBuild = false

// CheckE2EBuild refuses to run an e2e build without OPENCOZY_E2E_BUILD; a release build has
// nothing to check.
func CheckE2EBuild() error { return nil }

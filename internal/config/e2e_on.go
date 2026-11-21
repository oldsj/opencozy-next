//go:build e2e

package config

import (
	"errors"
	"os"
)

// E2EBuild reports a browser-test build (go build -tags e2e). Its calendar and single sign-on
// clients may fetch from 127.0.0.1 (and its OIDC issuer may be http there), so it must never
// serve a household.
const E2EBuild = true

// CheckE2EBuild refuses to run an e2e build unless OPENCOZY_E2E_BUILD=1 says it was meant to,
// so a test binary that ends up somewhere else does not start.
func CheckE2EBuild() error {
	if os.Getenv("OPENCOZY_E2E_BUILD") != "1" {
		return errors.New("this is an e2e build (its calendar and single sign-on clients may reach 127.0.0.1); it runs only in browser tests, with OPENCOZY_E2E_BUILD=1")
	}
	return nil
}

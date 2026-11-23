package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// Slice 0c: OPENCOZY_SECRET_KEYS and the private allowlists stop startup when wrong.
func TestLoadSecretsAndEgress(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/app")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecretKeys != nil || cfg.HomeAssistantAllowedPrivateCIDRs != nil || cfg.OIDCAllowedPrivateCIDRs != nil {
		t.Fatalf("unset should mean none: %v %v %v", cfg.SecretKeys, cfg.HomeAssistantAllowedPrivateCIDRs, cfg.OIDCAllowedPrivateCIDRs)
	}

	fictional := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	t.Setenv("OPENCOZY_SECRET_KEYS", "k1:"+fictional)
	t.Setenv("HOME_ASSISTANT_ALLOWED_PRIVATE_CIDRS", "192.168.77.0/24,fd00:77::/64")
	t.Setenv("OIDC_ALLOWED_PRIVATE_CIDRS", "10.20.0.0/16")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if ids := cfg.SecretKeys.IDs(); len(ids) != 1 || ids[0] != "k1" {
		t.Fatalf("keys %v", ids)
	}
	if len(cfg.HomeAssistantAllowedPrivateCIDRs) != 2 || len(cfg.OIDCAllowedPrivateCIDRs) != 1 {
		t.Fatalf("cidrs %v %v", cfg.HomeAssistantAllowedPrivateCIDRs, cfg.OIDCAllowedPrivateCIDRs)
	}

	for name, value := range map[string]string{
		"OPENCOZY_SECRET_KEYS":                 "k1:short",
		"HOME_ASSISTANT_ALLOWED_PRIVATE_CIDRS": "169.254.169.254/32",
		"OIDC_ALLOWED_PRIVATE_CIDRS":           "127.0.0.0/8",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			_, err := Load()
			if err == nil {
				t.Fatalf("%s=%s accepted", name, value)
			}
			if strings.Contains(err.Error(), fictional) {
				t.Fatal("the error shows a key")
			}
		})
	}
}

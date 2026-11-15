package httpx

import (
	"strings"
	"testing"
)

func TestLogSafe(t *testing.T) {
	for in, want := range map[string]string{
		"/children/abc":                 "/children/abc",
		"/x\nlevel=ERROR msg=forged":    "/xlevel=ERROR msg=forged",
		"/x\r\n\x1b[31mred\x7f\u0085":   "/x[31mred",
		"/café":                         "/café",
		strings.Repeat("a", 300):        strings.Repeat("a", 256),
		strings.Repeat("a", 255) + "éé": strings.Repeat("a", 255),
	} {
		if got := LogSafe(in); got != want {
			t.Errorf("LogSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

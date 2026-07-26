package settings

import "testing"

func TestContextLinkBaseAllowsHTTPSAndLoopbackHTTPOnly(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"https://grafana.example.com", "http://127.0.0.1:18081", "http://localhost:3000"} {
		if err := validateContextLinkBase(value); err != nil {
			t.Errorf("validateContextLinkBase(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"http://grafana:3000", "ftp://127.0.0.1", "https://user:password@example.com", "https://example.com?next=other"} {
		if err := validateContextLinkBase(value); err == nil {
			t.Errorf("validateContextLinkBase(%q) succeeded", value)
		}
	}
}

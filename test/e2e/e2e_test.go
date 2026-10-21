//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

func TestProxyCallsArgoCD(t *testing.T) {
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(diagnostics())
		}
	})
	resp, err := http.Get(proxyURL + "/api/v1/applications")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("applications via proxy: status %d", resp.StatusCode)
	}
}

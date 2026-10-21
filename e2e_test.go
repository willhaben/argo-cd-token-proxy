//go:build e2e

package main

import (
	"net/http"
	"os"
	"testing"
)

// TestE2EProxyCallsArgoCD calls the proxy container over HTTP. The container,
// Argo CD and Dex are started by .github/workflows/e2e.yml.
func TestE2EProxyCallsArgoCD(t *testing.T) {
	proxyURL := os.Getenv("E2E_PROXY_URL")
	if proxyURL == "" {
		t.Fatal("E2E_PROXY_URL is not set")
	}
	resp, err := http.Get(proxyURL + "/api/v1/applications")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("applications via proxy: status %d", resp.StatusCode)
	}
}

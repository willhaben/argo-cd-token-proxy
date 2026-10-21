//go:build e2e

package main

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// TestE2ETokenExchange needs a running Argo CD with a Dex "kubernetes" connector
// and the env vars read by loadConfig; see .github/workflows/e2e.yml.
func TestE2ETokenExchange(t *testing.T) {
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(cfg.baseURL)
	if err != nil {
		t.Fatal(err)
	}
	tokens := &tokenCache{exchange: newDexExchanger(cfg).exchange, now: time.Now}
	proxy := httptest.NewServer(newProxy(target, tokens))
	defer proxy.Close()

	if loggedIn(t, cfg.baseURL) {
		t.Fatal("argo cd reports a session without a token")
	}
	if !loggedIn(t, proxy.URL) {
		t.Fatal("argo cd does not accept the token exchanged through the proxy")
	}
}

func loggedIn(t *testing.T, baseURL string) bool {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/v1/session/userinfo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("userinfo via %s: status %d", baseURL, resp.StatusCode)
	}
	var body struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		t.Fatal(err)
	}
	return body.LoggedIn
}

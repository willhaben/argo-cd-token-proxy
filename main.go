// Command argo-cd-token-proxy forwards Argo CD API requests with a Dex token
// obtained by exchanging the pod's Kubernetes ServiceAccount token.
package main

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	tokenExchangeGrant = "urn:ietf:params:oauth:grant-type:token-exchange"
	subjectTokenType   = "urn:ietf:params:oauth:token-type:id_token"
	tokenScope         = "openid groups federated:id"
	dexTokenPath       = "/api/dex/token"
	exchangeTimeout    = 10 * time.Second
	maxRefreshMargin   = time.Minute
)

type config struct {
	baseURL     string
	tokenFile   string
	clientID    string
	connectorID string
	listenAddr  string
	debug       bool
}

func loadConfig() (config, error) {
	for _, name := range []string{"ARGOCD_BASE_URL", "ARGOCD_DEX_TOKEN_FILE", "ARGOCD_DEX_CLIENT_ID", "ARGOCD_DEX_CONNECTOR_ID"} {
		if os.Getenv(name) == "" {
			return config{}, fmt.Errorf("required environment variable %s is not set", name)
		}
	}
	return config{
		baseURL:     os.Getenv("ARGOCD_BASE_URL"),
		tokenFile:   os.Getenv("ARGOCD_DEX_TOKEN_FILE"),
		clientID:    os.Getenv("ARGOCD_DEX_CLIENT_ID"),
		connectorID: os.Getenv("ARGOCD_DEX_CONNECTOR_ID"),
		listenAddr:  cmp.Or(os.Getenv("LISTEN_ADDR"), ":8083"),
		debug:       os.Getenv("ARGOCD_DEX_DEBUG") == "true",
	}, nil
}

type dexToken struct {
	Value     string
	Source    string
	ExpiresIn time.Duration
}

type exchangeFunc func(ctx context.Context) (dexToken, error)

// tokenCache keeps one Dex token and exchanges again shortly before it expires.
type tokenCache struct {
	mu        sync.Mutex
	exchange  exchangeFunc
	now       func() time.Time
	token     string
	refreshAt time.Time
}

func (c *tokenCache) get(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.refreshAt) {
		return c.token, nil
	}
	fresh, err := c.exchange(ctx)
	if err != nil {
		c.token = ""
		return "", err
	}
	c.token = fresh.Value
	c.refreshAt = c.now().Add(refreshDelay(fresh.ExpiresIn))
	return c.token, nil
}

// refreshDelay returns how long a token may be used: its lifetime minus a
// margin of at most one minute.
func refreshDelay(expiresIn time.Duration) time.Duration {
	return expiresIn - min(maxRefreshMargin, expiresIn/10)
}

type dexExchanger struct {
	client      *http.Client
	endpoint    string
	tokenFile   string
	clientID    string
	connectorID string
	debug       bool
}

func newDexExchanger(cfg config) dexExchanger {
	return dexExchanger{
		client:      &http.Client{Timeout: exchangeTimeout},
		endpoint:    strings.TrimSuffix(cfg.baseURL, "/") + dexTokenPath,
		tokenFile:   cfg.tokenFile,
		clientID:    cfg.clientID,
		connectorID: cfg.connectorID,
		debug:       cfg.debug,
	}
}

func (d dexExchanger) exchange(ctx context.Context) (dexToken, error) {
	subject, err := os.ReadFile(d.tokenFile)
	if err != nil {
		return dexToken{}, fmt.Errorf("read subject token: %w", err)
	}
	form := url.Values{
		"grant_type":         {tokenExchangeGrant},
		"subject_token":      {strings.TrimSpace(string(subject))},
		"subject_token_type": {subjectTokenType},
		"connector_id":       {d.connectorID},
		"client_id":          {d.clientID},
		"scope":              {tokenScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return dexToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := d.client.Do(req)
	if err != nil {
		return dexToken{}, fmt.Errorf("dex token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if d.debug {
			body, _ := io.ReadAll(resp.Body)
			return dexToken{}, fmt.Errorf("dex token request: status %d: %s", resp.StatusCode, body)
		}
		return dexToken{}, fmt.Errorf("dex token request: status %d", resp.StatusCode)
	}

	var body struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		return dexToken{}, fmt.Errorf("decode dex token response: %w", err)
	}
	if body.ExpiresIn <= 0 {
		return dexToken{}, errors.New("dex token response has no expires_in")
	}

	token := dexToken{ExpiresIn: time.Duration(body.ExpiresIn) * time.Second, Source: "id_token", Value: body.IDToken}
	if token.Value == "" {
		token.Source, token.Value = "access_token", body.AccessToken
	}
	if token.Value == "" {
		return dexToken{}, errors.New("dex token response has no token")
	}
	slog.Info("dex returned token", "source", token.Source, "expires_in", token.ExpiresIn.String())
	return token, nil
}

func newProxy(target *url.URL, tokens *tokenCache) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := tokens.get(r.Context())
		if err != nil {
			slog.Error("dex token unavailable", "error", err)
			http.Error(w, "upstream authentication unavailable", http.StatusBadGateway)
			return
		}
		r.Header.Set("Authorization", "Bearer "+token)
		proxy.ServeHTTP(w, r)
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	cfg, err := loadConfig()
	if err != nil {
		fatal(err)
	}
	target, err := url.Parse(cfg.baseURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		fatal(fmt.Errorf("ARGOCD_BASE_URL must be an absolute URL, got %q", cfg.baseURL))
	}

	tokens := &tokenCache{exchange: newDexExchanger(cfg).exchange, now: time.Now}
	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           newProxy(target, tokens),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("listening", "addr", cfg.listenAddr, "target", target.Redacted())
	fatal(server.ListenAndServe())
}

func fatal(err error) {
	slog.Error("proxy stopped", "error", err)
	os.Exit(1)
}

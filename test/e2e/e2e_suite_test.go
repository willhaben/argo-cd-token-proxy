//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	clusterName   = "argo-cd-token-proxy-e2e"
	image         = "argo-cd-token-proxy:e2e"
	namespace     = "argo-cd"
	argoCDVersion = "10.10.5"
	proxyManifest = "testdata/proxy.yaml"
	argoCDValues  = "testdata/argo-cd-values.yaml"
)

var (
	proxyURL     string
	portForward  *exec.Cmd
	clusterOwned bool
)

func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

func runSuite(m *testing.M) int {
	defer teardown()
	if err := setup(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup failed: %v\n%s", err, diagnostics())
		return 1
	}
	return m.Run()
}

// setup creates a kind cluster unless one with the same name already exists,
// then installs Argo CD with Dex and deploys the proxy built from this repo.
func setup() error {
	if err := createCluster(); err != nil {
		return fmt.Errorf("create kind cluster: %w", err)
	}
	if err := installArgoCD(); err != nil {
		return fmt.Errorf("install argo cd: %w", err)
	}
	if _, err := run("docker", "build", "--tag", image, "../.."); err != nil {
		return fmt.Errorf("build proxy image: %w", err)
	}
	if _, err := run("kind", "load", "docker-image", image, "--name", clusterName); err != nil {
		return fmt.Errorf("load proxy image: %w", err)
	}
	if _, err := run("kubectl", "apply", "--filename", proxyManifest); err != nil {
		return fmt.Errorf("deploy proxy: %w", err)
	}
	if _, err := run("kubectl", "rollout", "status", "deployment/argo-cd-token-proxy", "--namespace", namespace, "--timeout", "2m"); err != nil {
		return fmt.Errorf("proxy rollout: %w", err)
	}
	return startPortForward()
}

// teardown stops the port-forward and deletes the cluster only if this run
// created it. E2E_KEEP_CLUSTER=true keeps it for debugging.
func teardown() {
	if portForward != nil {
		_ = portForward.Process.Kill()
		_ = portForward.Wait()
	}
	if clusterOwned && os.Getenv("E2E_KEEP_CLUSTER") != "true" {
		_, _ = run("kind", "delete", "cluster", "--name", clusterName)
	}
}

func createCluster() error {
	out, err := run("kind", "get", "clusters")
	if err != nil {
		return err
	}
	if slices.Contains(strings.Fields(out), clusterName) {
		return nil
	}
	clusterOwned = true
	_, err = run("kind", "create", "cluster", "--name", clusterName)
	return err
}

func installArgoCD() error {
	if err := ensure("kubectl", "create", "clusterrolebinding", "oidc-discovery-anonymous",
		"--clusterrole=system:service-account-issuer-discovery", "--group=system:unauthenticated"); err != nil {
		return err
	}
	if err := ensure("kubectl", "create", "namespace", namespace); err != nil {
		return err
	}
	if _, err := run("helm", "repo", "add", "--force-update", "argo", "https://argoproj.github.io/argo-helm"); err != nil {
		return err
	}
	_, err := run("helm", "upgrade", "--install", "argo-cd", "argo/argo-cd", "--version", argoCDVersion,
		"--namespace", namespace, "--values", argoCDValues, "--wait", "--timeout", "3m")
	return err
}

// startPortForward exposes the proxy on a free local port and waits until it
// answers HTTP.
func startPortForward() error {
	port, err := freePort()
	if err != nil {
		return err
	}
	portForward = exec.Command("kubectl", "port-forward", "--namespace", namespace,
		"service/argo-cd-token-proxy", fmt.Sprintf("%d:8083", port))
	if err := portForward.Start(); err != nil {
		return fmt.Errorf("start port-forward: %w", err)
	}
	proxyURL = fmt.Sprintf("http://localhost:%d", port)
	client := http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(time.Minute)
	for {
		resp, err := client.Get(proxyURL + "/")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("port-forward not ready: %w", err)
		}
		time.Sleep(time.Second)
	}
}

// diagnostics collects the state of the Argo CD and proxy workloads for a
// failed run.
func diagnostics() string {
	var b strings.Builder
	for _, args := range [][]string{
		{"get", "pods", "--namespace", namespace, "--output", "wide"},
		{"get", "events", "--namespace", namespace, "--sort-by=.lastTimestamp"},
		{"logs", "deployment/argo-cd-token-proxy", "--namespace", namespace},
		{"logs", "deployment/argo-cd-argocd-dex-server", "--namespace", namespace},
		{"logs", "deployment/argo-cd-argocd-server", "--namespace", namespace},
	} {
		out, _ := run("kubectl", args...)
		fmt.Fprintf(&b, "$ kubectl %s\n%s\n", strings.Join(args, " "), out)
	}
	return b.String()
}

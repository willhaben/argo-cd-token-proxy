# argo-cd-token-proxy

Sidecar for the Argo CD MCP pod. It accepts plain Argo CD API requests on a
local port, adds a Dex bearer token, and forwards them to Argo CD.

## Request flow

1. The MCP sends a plain request to the sidecar (no Authorization header needed).
2. The sidecar returns its cached Dex token, or exchanges one first:
   RFC 8693 token exchange at `ARGOCD_BASE_URL/api/dex/token`, using the pod's
   Kubernetes ServiceAccount token (re-read from `ARGOCD_DEX_TOKEN_FILE` on every
   exchange, since the kubelet rotates it).
3. The token is refreshed shortly before its `expires_in`. A failed exchange
   returns 502 without token details, and the next request retries.
4. Any incoming Authorization header is replaced with `Bearer <token>`, and the
   request is forwarded to Argo CD.

The `id_token` from Dex is used when present, otherwise the `access_token`.
The chosen field is logged by name; token values are never logged.

## Configuration

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `ARGOCD_BASE_URL` | yes | | e.g. `http://argo-cd-argocd-server.argo-cd.svc.cluster.local` |
| `ARGOCD_DEX_TOKEN_FILE` | yes | | e.g. `/var/run/secrets/dex-subject-token/token` |
| `ARGOCD_DEX_CLIENT_ID` | yes | | No default. |
| `ARGOCD_DEX_CONNECTOR_ID` | yes | | No default. |
| `LISTEN_ADDR` | no | `:8083` | |
| `ARGOCD_DEX_DEBUG` | no | `false` | `true` logs the full body of a failed Dex token response. Successful responses are never logged. |

The process refuses to start and names the variable if a required one is missing.

## Dex client requirement

The client named by `ARGOCD_DEX_CLIENT_ID` must be a public Dex client with no
secret. The proxy sends `client_id` as a form field and no client secret and no
Basic auth.

## Build

```sh
docker build -t argo-cd-token-proxy .
go test ./...
```

The e2e suite (`test/e2e`, build tag `e2e`) creates a kind cluster, installs
Argo CD with a Dex Kubernetes connector, deploys the proxy image, and runs the
test against it. It needs `docker`, `kind`, `kubectl` and `helm` on the PATH:

```sh
go test -tags e2e -v -count=1 ./test/e2e/
```

The suite deletes the cluster it created. Set `E2E_KEEP_CLUSTER=true` to keep
it for debugging. An existing cluster named `argo-cd-token-proxy-e2e` is reused
and left alone.

Images are pushed to `ghcr.io/willhaben/argo-cd-token-proxy` by the
`build-image` workflow on pushes to `main`, tagged with the commit SHA and `latest`.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository overview

Go library (`github.com/kubetail-org/grpc-dispatcher-go`) that fans out gRPC queries to multiple servers behind a Kubernetes service. The library itself lives at the repo root; `example/` is a separate Go module with a demo app/server used by the Tilt dev environment.

## Commands

```console
go test -race ./...           # run full test suite with race detector (matches CI)
go test -race -run TestX ./   # run a single test
go vet ./...                  # CI runs this
test -z $(gofmt -l .)         # CI lint check — fails if any file is unformatted
```

Integration tests (`integration_test.go`, gated behind `//go:build integration`) need a live Kubernetes cluster — the default `go test ./...` skips them. To run locally:

```console
kind create cluster --image kindest/node:v1.30.4 --name dispatcher-it
KUBECONFIG=~/.kube/config go test -tags=integration -race -v -timeout=5m ./...
kind delete cluster --name dispatcher-it
```

CI runs them across a matrix of supported k8s versions (1.21.x through 1.33.x — see `.github/workflows/ci.yml` for exact pins).

Dev environment (requires a Kubernetes cluster + Tilt + ctlptl):

```console
ctlptl apply -f hack/ctlptl/minikube.yaml   # bring up dev cluster
tilt up                                      # run example app on http://localhost:4000
tilt down && ctlptl delete -f hack/ctlptl/minikube.yaml   # teardown
```

Regenerating protobuf stubs (from `example/`): `go generate ./...` (needs `protoc-gen-go` and `protoc-gen-go-grpc` installed).

CI uses Go 1.23.4; `go.mod` declares 1.24.0 with toolchain 1.24.5 — keep that gap in mind when using newer language features.

## Architecture

The dispatcher is a thin layer on top of standard gRPC client machinery. Three pieces work together:

1. **EndpointSlice informer** (`dispatcher.go`) — a `client-go` SharedInformer watches `discovery.k8s.io/v1` EndpointSlices filtered by `kubernetes.io/service-name`. Add/Update/Delete handlers diff old vs new endpoints and call `updateState`, which (a) mutates the in-memory `mapset.Set[server]`, (b) pushes the new address list into the manual resolver, and (c) publishes `add:servers` events on an in-process EventBus so subscribers can react to newly available pods.

2. **Custom gRPC balancer** (`balancer.go`) — registered under a randomized name (`dispatcher_balancer-<rand>`) so multiple dispatchers in the same process don't collide. The picker reads a target IP from `context.Value(dispatcherAddrCtxKey)` set by each dispatch method and returns the matching SubConn. If no SubConn is ready, it returns `ErrNoSubConnAvailable` — combined with the dispatcher's `WaitForReady(true)` default, this lets calls block until a pod becomes available rather than failing fast.

3. **Dispatch surface** (`dispatcher.go`) — five entry points with distinct semantics:
   - `Unicast` — one-shot to the pod on a given node, returns when the handler finishes or ctx is done.
   - `UnicastSubscribe` — runs handler against current matching server *and* every future matching server until `Unsubscribe()`.
   - `UnicastSubscribeOnce` — wait-for-availability variant; fires exactly once when a matching server appears or ctx cancels.
   - `Fanout` — one-shot to all current servers in parallel; waits for all handlers or ctx.
   - `FanoutSubscribe` — same as `UnicastSubscribe` but fires for every server, not just one node.

   All five route through the single `*grpc.ClientConn`; per-call routing happens via the context key the picker reads.

Subscriptions use a `serverCh` plus `done` channel pattern. `Subscription.Unsubscribe` is idempotent (`sync.Once`) and orders cleanup carefully: unsubscribe from the EventBus first, then close `done`, so no further sends race the close.

The dispatcher requires the gRPC servers to expose pod IPs in EndpointSlice `endpoints[].addresses` — it ignores `notReady`/`terminating` endpoints by checking `Conditions.Serving`. RBAC: needs `list` and `watch` on `endpointslices`.

## Testing notes

`dispatcher_test.go` uses `k8s.io/client-go/kubernetes/fake` to stand in for the API server — pass it via `WithKubernetesClientset`. There is no real network in tests; the `pickerBuilder` and resolver paths are exercised by injecting fake EndpointSlices into the informer's store.

`integration_test.go` (build tag `integration`) is the cross-version harness. It runs one in-process gRPC server bound to `0.0.0.0:<random-port>` and creates EndpointSlices with multiple endpoints all pointing at the test runner's outbound IP (different node names) — all dispatch calls reach the same listener. Loopback addresses (`127.0.0.0/8`) and link-local (`169.254.0.0/16`) are rejected by apiserver validation, which is why we use the host's routable IP via the UDP-dial trick in `hostIP()`. Reads `KUBECONFIG` from env.

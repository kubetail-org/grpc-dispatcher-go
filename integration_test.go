//go:build integration

// Copyright 2024 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Integration tests run against a real Kubernetes apiserver (kind in CI).
// They use a single in-process gRPC server bound to 0.0.0.0:<random-port>
// and put the test runner's outbound IP into multiple EndpointSlice endpoints
// (with distinct node names). All "backends" reach the same listener — we are
// testing EndpointSlice handling and dispatch routing, not multi-backend
// isolation. The apiserver rejects loopback/link-local addresses in
// EndpointSlices, which is why we use the host's routable IP. Requires
// KUBECONFIG to point at a live cluster.

package grpcdispatcher

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// k8sClient builds a clientset using the default loading rules (KUBECONFIG env
// var, then ~/.kube/config).
func k8sClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), nil,
	).ClientConfig()
	if err != nil {
		t.Fatalf("failed to load kubeconfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	return cs
}

// startGRPCServer binds one gRPC server to 0.0.0.0:<random> and returns the port.
// The health service is registered so dispatch handlers have something to call.
func startGRPCServer(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	s := grpc.NewServer()
	healthpb.RegisterHealthServer(s, health.NewServer())
	go s.Serve(lis)
	t.Cleanup(s.GracefulStop)
	return lis.Addr().(*net.TCPAddr).Port
}

// hostIP returns the runner's outbound IP — a routable, non-loopback address
// that the apiserver will accept in EndpointSlice.endpoints[].addresses and
// that the runner can dial back to itself (since the gRPC server binds 0.0.0.0).
// The UDP "dial" doesn't send packets; it just resolves which local IP the OS
// would use for the destination.
func hostIP(t *testing.T) string {
	t.Helper()
	conn, err := net.Dial("udp", "8.8.8.8:80")
	require.NoError(t, err)
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// createNamespace makes a unique namespace and registers cleanup.
func createNamespace(t *testing.T, cs kubernetes.Interface) string {
	t.Helper()
	ns := fmt.Sprintf("dispatcher-it-%d", time.Now().UnixNano())
	_, err := cs.CoreV1().Namespaces().Create(context.Background(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = cs.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})
	return ns
}

// createServiceAndSlice creates a headless Service plus an EndpointSlice with
// one endpoint per (ip, nodeName) pair. All endpoints share the given port.
func createServiceAndSlice(t *testing.T, cs kubernetes.Interface, ns, svcName string, port int32, endpoints []discoveryv1.Endpoint) {
	t.Helper()
	ctx := context.Background()

	_, err := cs.CoreV1().Services(ns).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: svcName},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports:     []corev1.ServicePort{{Port: port}},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	portName := "grpc"
	_, err = cs.DiscoveryV1().EndpointSlices(ns).Create(ctx, &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:   svcName + "-abc",
			Labels: map[string]string{discoveryv1.LabelServiceName: svcName},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{{
			Name: &portName,
			Port: &port,
		}},
		Endpoints: endpoints,
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}

func endpoint(ip, nodeName string) discoveryv1.Endpoint {
	ready := true
	nn := nodeName
	return discoveryv1.Endpoint{
		Addresses: []string{ip},
		NodeName:  &nn,
		// Serving is alpha in 1.21 (behind EndpointSliceTerminatingCondition gate) and may
		// be stripped by the API server; set Ready as a fallback that older apiservers preserve.
		Conditions: discoveryv1.EndpointConditions{Serving: &ready, Ready: &ready},
	}
}

// waitForServers blocks until the dispatcher's informer has registered the
// expected number of backends, or the context expires.
func waitForServers(t *testing.T, d *Dispatcher, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		got := d.servers.Cardinality()
		d.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dispatcher never saw %d servers", want)
}

func newDispatcher(t *testing.T, cs kubernetes.Interface, ns, svcName string, port int) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(
		fmt.Sprintf("kubernetes://%s.%s:%d", svcName, ns, port),
		WithKubernetesClientset(cs),
		WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	require.NoError(t, err)
	d.Start()
	t.Cleanup(func() { _ = d.Shutdown() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d.Ready(ctx)
	return d
}

func TestIntegrationFanout(t *testing.T) {
	cs := k8sClient(t)
	ns := createNamespace(t, cs)
	port := startGRPCServer(t)
	ip := hostIP(t)

	createServiceAndSlice(t, cs, ns, "svc", int32(port), []discoveryv1.Endpoint{
		endpoint(ip, "node-1"),
		endpoint(ip, "node-2"),
		endpoint(ip, "node-3"),
	})

	d := newDispatcher(t, cs, ns, "svc", port)
	waitForServers(t, d, 3, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	calls := 0
	d.Fanout(ctx, func(hctx context.Context, conn *grpc.ClientConn) {
		// Verify gRPC actually reaches the in-process server.
		hc := healthpb.NewHealthClient(conn)
		resp, err := hc.Check(hctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		require.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.Status)

		require.Equal(t, fmt.Sprintf("%s:%d", ip, port), hctx.Value(dispatcherAddrCtxKey).(string))
		mu.Lock()
		calls++
		mu.Unlock()
	})
	require.Equal(t, 3, calls)
}

func TestIntegrationUnicastByNode(t *testing.T) {
	cs := k8sClient(t)
	ns := createNamespace(t, cs)
	port := startGRPCServer(t)
	ip := hostIP(t)

	createServiceAndSlice(t, cs, ns, "svc", int32(port), []discoveryv1.Endpoint{
		endpoint(ip, "node-1"),
		endpoint(ip, "node-2"),
	})

	d := newDispatcher(t, cs, ns, "svc", port)
	waitForServers(t, d, 2, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	called := false
	d.Unicast(ctx, "node-2", func(hctx context.Context, conn *grpc.ClientConn) {
		hc := healthpb.NewHealthClient(conn)
		_, err := hc.Check(hctx, &healthpb.HealthCheckRequest{})
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("%s:%d", ip, port), hctx.Value(dispatcherAddrCtxKey).(string))
		called = true
	})
	require.True(t, called, "unicast handler did not fire for node-2")
}

func TestIntegrationSubscribeSeesNewEndpoint(t *testing.T) {
	cs := k8sClient(t)
	ns := createNamespace(t, cs)
	port := startGRPCServer(t)
	ip := hostIP(t)

	createServiceAndSlice(t, cs, ns, "svc", int32(port), []discoveryv1.Endpoint{
		endpoint(ip, "node-1"),
	})

	d := newDispatcher(t, cs, ns, "svc", port)
	waitForServers(t, d, 1, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	seenNodes := mapset.NewSet[string]()
	gotCh := make(chan struct{}, 4)

	sub, err := d.FanoutSubscribe(ctx, func(hctx context.Context, conn *grpc.ClientConn) {
		require.Equal(t, fmt.Sprintf("%s:%d", ip, port), hctx.Value(dispatcherAddrCtxKey).(string))
		gotCh <- struct{}{}
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Wait for the initial server, then snapshot the dispatcher's view of nodes.
	select {
	case <-gotCh:
	case <-ctx.Done():
		t.Fatal("never saw initial server")
	}
	mu.Lock()
	for s := range d.servers.Iter() {
		seenNodes.Add(s.nodeName)
	}
	mu.Unlock()
	require.True(t, seenNodes.Contains("node-1"))

	// Patch the EndpointSlice to add a second endpoint.
	es, err := cs.DiscoveryV1().EndpointSlices(ns).Get(ctx, "svc-abc", metav1.GetOptions{})
	require.NoError(t, err)
	es.Endpoints = append(es.Endpoints, endpoint(ip, "node-2"))
	_, err = cs.DiscoveryV1().EndpointSlices(ns).Update(ctx, es, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Wait for the new server to fire the subscription.
	select {
	case <-gotCh:
	case <-ctx.Done():
		t.Fatal("never saw new server after EndpointSlice update")
	}
	waitForServers(t, d, 2, 5*time.Second)

	mu.Lock()
	defer mu.Unlock()
	seenNodes.Clear()
	for s := range d.servers.Iter() {
		seenNodes.Add(s.nodeName)
	}
	require.True(t, seenNodes.Contains("node-1"))
	require.True(t, seenNodes.Contains("node-2"))
}

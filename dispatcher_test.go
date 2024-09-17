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

package grpcdispatcher

import (
	"context"
	"sync"
	"testing"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/client-go/kubernetes/fake"
)

const testCtxKey key = "testkey"

func newTestDispatcher() *Dispatcher {
	d, _ := NewDispatcher(
		"kubernetes://my-service.my-namespace",
		WithKubernetesClientset(fake.NewSimpleClientset()),
		WithDialOptions(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	return d
}

func TestParseConnectUrl(t *testing.T) {
	tests := []struct {
		name            string
		setUrl          string
		wantNamespace   string
		wantServiceName string
		wantPort        string
	}{
		{
			"default port",
			"kubernetes://my-service.my-namespace",
			"my-namespace",
			"my-service",
			"50051",
		},
		{
			"custom port number",
			"kubernetes://my-service.my-namespace:1234",
			"my-namespace",
			"my-service",
			"1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := parseConnectUrl(tt.setUrl)
			require.Nil(t, err)
			require.Equal(t, tt.wantNamespace, args.Namespace)
			require.Equal(t, tt.wantServiceName, args.ServiceName)
			require.Equal(t, tt.wantPort, args.Port)
		})
	}
}

func TestDispatcherUpdateState(t *testing.T) {
	initialIps := []string{"ip1", "ip2"}

	tests := []struct {
		name        string
		setToAdd    []string
		setToDelete []string
		wantIps     mapset.Set[string]
	}{
		{
			"add ips",
			[]string{"ip3", "ip4"},
			[]string{},
			mapset.NewSet("ip1", "ip2", "ip3", "ip4"),
		},
		{
			"delete ips",
			[]string{},
			[]string{"ip2"},
			mapset.NewSet("ip1"),
		},
		{
			"add and delete ips",
			[]string{"ip3", "ip4"},
			[]string{"ip2"},
			mapset.NewSet("ip1", "ip3", "ip4"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// initialize dispatcher
			d := newTestDispatcher()
			d.updateState(initialIps, nil)

			// apply update
			d.updateState(tt.setToAdd, tt.setToDelete)

			// check result
			require.Equal(t, tt.wantIps, d.ips)
		})
	}
}

func TestDispatcherFanout(t *testing.T) {
	setIps := []string{"ip1", "ip2"}
	wantIps := mapset.NewSet("ip1:50051", "ip2:50051")

	// initialize dispatcher
	d := newTestDispatcher()
	d.updateState(setIps, nil)

	rootCtx := context.WithValue(context.Background(), testCtxKey, "yyy")

	// execute fanout query
	var mu sync.Mutex
	ips := []string{}
	d.Fanout(rootCtx, func(ctx context.Context, clientconn *grpc.ClientConn) {
		// check context inheritance
		ctxVal := ctx.Value(testCtxKey).(string)
		require.Equal(t, "yyy", ctxVal)

		// add ip to results
		ip := ctx.Value(dispatcherAddrCtxKey).(string)
		mu.Lock()
		ips = append(ips, ip)
		mu.Unlock()
	})

	// check result
	require.Equal(t, wantIps, mapset.NewSet(ips...))
}

func TestDispatcherFanoutSubscribe(t *testing.T) {
	setInitialIps := []string{"ip1", "ip2"}
	wantIps := mapset.NewSet("ip1:50051", "ip2:50051", "ip3:50051")

	// initialize dispatcher
	d := newTestDispatcher()
	d.updateState(setInitialIps, nil)

	rootCtx := context.WithValue(context.Background(), testCtxKey, "yyy")

	// execute fanout query
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(3)

	ips := []string{}
	sub, err := d.FanoutSubscribe(rootCtx, func(ctx context.Context, clientconn *grpc.ClientConn) {
		defer wg.Done()
		// check context inheritance
		ctxVal := ctx.Value(testCtxKey).(string)
		require.Equal(t, "yyy", ctxVal)

		// add ip to results
		ip := ctx.Value(dispatcherAddrCtxKey).(string)
		mu.Lock()
		ips = append(ips, ip)
		mu.Unlock()
	})
	require.Nil(t, err)
	defer sub.Unsubscribe()

	// add another ip after subscription has started
	d.updateState([]string{"ip3"}, nil)

	wg.Wait()

	// check result
	require.Equal(t, wantIps, mapset.NewSet(ips...))
}

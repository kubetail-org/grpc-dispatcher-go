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
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/balancer"
	resolver "google.golang.org/grpc/resolver"
)

type mockSubConn struct {
	mock.Mock
}

func (m *mockSubConn) UpdateAddresses([]resolver.Address) {}
func (m *mockSubConn) Connect()                           {}
func (m *mockSubConn) GetOrBuildProducer(b balancer.ProducerBuilder) (balancer.Producer, func()) {
	panic("not implemented")
}
func (m *mockSubConn) Shutdown() {}

func TestPicker_Pick(t *testing.T) {
	subConns := map[string]balancer.SubConn{
		"192.168.1.1": &mockSubConn{},
		"192.168.1.2": &mockSubConn{},
	}

	p := &picker{
		subConns: subConns,
	}

	tests := []struct {
		name        string
		ctxValue    string
		expectedErr error
	}{
		{
			name:        "SubConn exists for IP",
			ctxValue:    "192.168.1.1",
			expectedErr: nil,
		},
		{
			name:        "SubConn does not exist for IP",
			ctxValue:    "192.168.1.3",
			expectedErr: errors.New("subconn for ip 192.168.1.3 not ready"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), dispatcherAddrCtxKey, tt.ctxValue)
			info := balancer.PickInfo{
				Ctx: ctx,
			}

			_, err := p.Pick(info)
			if tt.expectedErr == nil {
				require.Nil(t, err)
			} else {
				require.Error(t, tt.expectedErr, err)
			}
		})
	}
}

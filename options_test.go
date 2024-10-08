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
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDispatcherOptionsDefaults(t *testing.T) {
	opts := newDispatcherOptions()
	assert.Equal(t, []grpc.DialOption{}, opts.dialOpts)
	assert.Nil(t, opts.clientset)
}

func TestWithDialOptions(t *testing.T) {
	wantDialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithAuthority("xxx"),
	}

	opts := newDispatcherOptions(WithDialOptions(wantDialOpts...))
	assert.Equal(t, wantDialOpts, opts.dialOpts)
}

func TestWithKubernetesClientset(t *testing.T) {
	wantClientset := fake.NewClientset()

	opts := newDispatcherOptions(WithKubernetesClientset(wantClientset))
	assert.Equal(t, wantClientset, opts.clientset)
}

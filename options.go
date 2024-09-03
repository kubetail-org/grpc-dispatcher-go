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
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
)

// Represents options for dispatcher
type dispatcherOptions struct {
	dialOpts  []grpc.DialOption
	clientset kubernetes.Interface
}

// DispatcherOption configures how we set up the dispatcher
type DispatcherOption func(*dispatcherOptions)

// Returns new dispatcherOptions instance
func newDispatcherOptions(optFns ...DispatcherOption) *dispatcherOptions {
	// initialize
	opts := &dispatcherOptions{
		dialOpts: []grpc.DialOption{},
	}

	// apply option functions
	for _, optFn := range optFns {
		optFn(opts)
	}

	return opts
}

// WithDialOptions configures the DialOptions to use when initializing
// a new grpc connection
func WithDialOptions(dialOpts ...grpc.DialOption) DispatcherOption {
	return func(opts *dispatcherOptions) {
		opts.dialOpts = dialOpts
	}
}

// WithKubernetesClientset configures the dispatcher to use the
// clientset when querying Kubernetes API
func WithKubernetesClientset(clientset kubernetes.Interface) DispatcherOption {
	return func(opts *dispatcherOptions) {
		opts.clientset = clientset
	}
}

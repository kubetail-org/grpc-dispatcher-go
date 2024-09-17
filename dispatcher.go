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
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	eventbus "github.com/asaskevich/EventBus"
	mapset "github.com/deckarep/golang-set/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	resolver "google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type key string

const dispatcherAddrCtxKey key = "dispatcherAddr"

var balancerName string

func init() {
	balancerName = fmt.Sprintf("dispatcher_balancer-%d", rand.Intn(100000))
	builder := base.NewBalancerBuilder(balancerName, &pickerBuilder{}, base.Config{HealthCheck: true})
	balancer.Register(builder)
}

// Represents the callback argument to the dispatch methods
type DispatchHandler func(ctx context.Context, conn *grpc.ClientConn)

// Represents interest in pod ips that are part of a Kubernetes service
type Subscription struct {
	ipCh    chan string
	cleanup func()
}

// Ends subscription
func (sub *Subscription) Unsubscribe() {
	close(sub.ipCh)
	sub.cleanup()
}

// A Dispatcher is a utility that facilitates sending queries to multiple grpc servers
// simultaneously. It maintains an up-to-date list of all pod ips that are part of a
// Kubernetes service and directs queries to the ips.
type Dispatcher struct {
	opts        *dispatcherOptions
	connectArgs *connectArgs
	informer    cache.SharedInformer
	informerReg cache.ResourceEventHandlerRegistration
	resolver    *manual.Resolver
	conn        *grpc.ClientConn
	ips         mapset.Set[string]
	mu          sync.Mutex
	eventbus    eventbus.Bus
	stopCh      chan struct{}
}

// Sends queries to all available ips at query-time
func (d *Dispatcher) Fanout(ctx context.Context, fn DispatchHandler) {
	var wg sync.WaitGroup

	d.mu.Lock()
	ips := d.ips.ToSlice()
	d.mu.Unlock()

	for _, ip := range ips {
		connCtx := context.WithValue(ctx, dispatcherAddrCtxKey, fmt.Sprintf("%s:%s", ip, d.connectArgs.Port))
		wg.Add(1)
		go func(lclCtx context.Context) {
			defer wg.Done()
			fn(lclCtx, d.conn)
		}(connCtx)
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	// wait for funcs or ctx to finish whichever comes first
	select {
	case <-ctx.Done():
	case <-doneCh:
	}
}

// Sends queries to all available ips at query-time and all subsequent ips when
// they become available until Unsubscribe() is called
func (d *Dispatcher) FanoutSubscribe(ctx context.Context, fn DispatchHandler) (*Subscription, error) {
	ipCh := make(chan string)

	// ip handler
	handleNewIps := func(newIps []string) {
		for _, ip := range newIps {
			ipCh <- ip
		}
	}

	// worker
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ip, ok := <-ipCh:
				if !ok {
					// unsubscribe was called
					return
				}

				// execute dispatch handler in goroutine
				connCtx := context.WithValue(ctx, dispatcherAddrCtxKey, fmt.Sprintf("%s:%s", ip, d.connectArgs.Port))
				go fn(connCtx, d.conn)
			}
		}
	}()

	// get current ips and subscribe to new ones in a lock
	d.mu.Lock()
	currentIps := d.ips.ToSlice()
	err := d.eventbus.SubscribeAsync("add:addrs", handleNewIps, false)
	if err != nil {
		d.mu.Unlock()
		return nil, err
	}
	d.mu.Unlock()

	handleNewIps(currentIps)

	return &Subscription{
		ipCh: ipCh,
		cleanup: func() {
			d.eventbus.Unsubscribe("add:addrs", handleNewIps)
		},
	}, nil
}

// Start background processes and initialize subconns
func (d *Dispatcher) Start() {
	// exit if already started
	if d.stopCh != nil {
		return
	}

	// add event handler
	reg, _ := d.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			es := obj.(*discoveryv1.EndpointSlice)
			d.handleAddEndpointSlice(es)
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			esOld := oldObj.(*discoveryv1.EndpointSlice)
			esNew := newObj.(*discoveryv1.EndpointSlice)
			d.handleUpdateEndpointSlice(esOld, esNew)
		},
	})
	d.informerReg = reg

	// start informer and connect subconns
	d.stopCh = make(chan struct{})
	go d.informer.Run(d.stopCh)
}

// Wait until informer has synced with Kubernetes API
func (d *Dispatcher) Ready(ctx context.Context) {
	cache.WaitForCacheSync(ctx.Done(), d.informerReg.HasSynced)
}

// Closes connection which also stops resolver background processes
func (d *Dispatcher) Shutdown() error {
	if d.stopCh != nil {
		// stop informer
		close(d.stopCh)

		// remove event handler
		d.informer.RemoveEventHandler(d.informerReg)
	}
	return d.conn.Close()
}

// Handle add
func (d *Dispatcher) handleAddEndpointSlice(es *discoveryv1.EndpointSlice) {
	newIps := getIpsFromEndpointSlice(es)
	d.updateState(newIps, nil)
}

// Handle updates
func (d *Dispatcher) handleUpdateEndpointSlice(esOld *discoveryv1.EndpointSlice, esNew *discoveryv1.EndpointSlice) {
	oldIps := mapset.NewSet(getIpsFromEndpointSlice(esOld)...)
	newIps := mapset.NewSet(getIpsFromEndpointSlice(esNew)...)

	toDelete := oldIps.Difference(newIps)
	toAdd := newIps.Difference(oldIps)

	d.updateState(toAdd.ToSlice(), toDelete.ToSlice())
}

// Adds and deletes ips, updates clientconn state, publishes change to eventbus
func (d *Dispatcher) updateState(toAdd []string, toDelete []string) {
	d.mu.Lock()

	// update local state
	if len(toDelete) > 0 {
		d.ips.RemoveAll(toDelete...)
	}

	if len(toAdd) > 0 {
		d.ips.Append(toAdd...)
	}

	// exit if no changes
	if len(toAdd) == 0 && len(toDelete) == 0 {
		d.mu.Unlock()
		return
	}

	// update clientconn state
	ips := d.ips.ToSlice()

	addrs := make([]resolver.Address, len(ips))
	for i, ip := range ips {
		addrs[i] = resolver.Address{Addr: fmt.Sprintf("%s:%s", ip, d.connectArgs.Port)}
	}

	d.resolver.UpdateState(resolver.State{Addresses: addrs})

	d.mu.Unlock()

	// publish change
	if len(toAdd) > 0 {
		d.eventbus.Publish("add:addrs", toAdd)
	}
}

func NewDispatcher(connectUrl string, options ...DispatcherOption) (*Dispatcher, error) {
	// init opts
	opts := newDispatcherOptions(options...)

	// init clientset
	var clientset kubernetes.Interface
	if opts.clientset != nil {
		clientset = opts.clientset
	} else {
		k8scfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		clientset, err = kubernetes.NewForConfig(k8scfg)
		if err != nil {
			return nil, err
		}
	}

	// get service name and namespace
	connectArgs, err := parseConnectUrl(connectUrl)
	if err != nil {
		return nil, err
	}

	// init informer with 10 minute resync period
	labelSelector := labels.Set{
		discoveryv1.LabelServiceName: connectArgs.ServiceName,
	}.String()

	informer := cache.NewSharedInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = labelSelector
				return clientset.DiscoveryV1().EndpointSlices(connectArgs.Namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelSelector
				return clientset.DiscoveryV1().EndpointSlices(connectArgs.Namespace).Watch(context.TODO(), options)
			},
		},
		&discoveryv1.EndpointSlice{},
		10*time.Minute,
	)

	// init resolver
	resolver := manual.NewBuilderWithScheme("kubernetes")

	// init conn
	dialOpts := append(opts.dialOpts,
		grpc.WithResolvers(resolver),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, balancerName)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	conn, err := grpc.NewClient(connectUrl, dialOpts...)
	if err != nil {
		return nil, err
	}

	// eager connect
	conn.Connect()

	return &Dispatcher{
		opts:        opts,
		connectArgs: connectArgs,
		informer:    informer,
		resolver:    resolver,
		conn:        conn,
		ips:         mapset.NewSet[string](),
		eventbus:    eventbus.New(),
	}, nil
}

type connectArgs struct {
	Namespace   string
	ServiceName string
	Port        string
}

// Parse connect url and return connect args
func parseConnectUrl(connectUrl string) (*connectArgs, error) {
	u, err := url.Parse(connectUrl)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(u.Hostname(), ".")

	serviceName := parts[0]

	// get namespace
	var namespace string
	if len(parts) > 1 {
		namespace = parts[1]
	} else {
		nsPathname := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		nsBytes, err := os.ReadFile(nsPathname)
		if err != nil {
			return nil, fmt.Errorf("unable to read current namespace from %s: %v", nsPathname, err)
		}
		namespace = string(nsBytes)
	}

	// get port
	port := u.Port()
	if port == "" {
		port = "50051"
	}

	return &connectArgs{
		Namespace:   namespace,
		ServiceName: serviceName,
		Port:        port,
	}, nil
}

func getIpsFromEndpointSlice(es *discoveryv1.EndpointSlice) []string {
	var ips []string
	for _, endpoint := range es.Endpoints {
		if *endpoint.Conditions.Serving {
			ips = append(ips, endpoint.Addresses...)
		}
	}
	return ips
}

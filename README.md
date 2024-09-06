# gRPC-Dispatcher

gRPC-Dispatcher is a Go library for dispatching queries to multiple gRPC servers simultaneously (currently Kubernetes-only)

## Introduction

While working on gRPC client/server communications for [Kubetail](https://github.com/kubetail-org/kubetail) we realized that we had a need for fanout-type queries to all available servers and that those types of queries weren't directly supported by the core gRPC library or by any third-party libraries we could find. So, to make it easier to implement these types of queries, we created gRPC-Dispatcher. Currently, the library supports gRPC servers running behind a Kubernetes service and it has the following features:

* Uses Kubernetes API to get server ips and watch for changes in realtime
* Connects to new servers eagerly in background so new queries are instantaneous
* Supports long-running streaming queries to ephemeral servers

Try it out and let us know what you think! If you notice any bugs or have any feature requests just create a GitHub Issue.

## Quickstart

First, add the library to your Go project:
```console
go get github.com/kubetail-org/grpc-dispatcher-go
```

Next write your dispatch code:
```go
import (
  "context"

  "github.com/kubetail-org/grpc-dispatcher-go"
	"google.golang.org/grpc"
)

func main() {
  // initialize new dispatcher
  dispatcher, err := grpcdispatcher.NewDispatcher(
    "kubernetes://my-service.my-namespace",
    grpcdispatcher.WithDialOptions(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
  )
  if err != nil {
    return
  }
  defer dispatcher.Shutdown()

  // start background processes and initialize connections to servers
  dispatcher.Start()
  
  // wait until dispatcher is ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dispatcher.Ready(ctx)

  // all handler contexts will inherit from this context
  rootCtx := context.Background()
  
  // send query to all current grpc servers
  dispatcher.Fanout(rootCtx, func(ctx context.Context, conn *grpc.ClientConn) {
    // init grpc client
		client := examplepb.NewExampleServiceClient(conn)

		// execute grpc request
		resp, err := client.Echo(ctx, &examplepb.EchoRequest{Message: "hello"})
		if err != nil {
      // do something with error
			fmt.Println(err)
			return
		}

    // do something with response
    fmt.Println(resp)
  })
  
  // send query to all current and future grpc servers
  sub, err := dispatcher.FanoutSubscribe(rootCtx, func(ctx context.Context, conn *grpc.ClientConn) error {
    // init grpc client
		client := examplepb.NewExampleServiceClient(conn)

		// execute grpc request
		resp, err := client.Echo(ctx, &examplepb.EchoRequest{Message: "hello"})
		if err != nil {
      // do something with error
			fmt.Println(err)
			return
		}

    // do something with response
    fmt.Println(resp)
  })
  if err != nil {
    panic(err)
  }
  defer sub.Unsubscribe()
}
```

## Example

You can see an example implementatio in the [`example/`](example/) directory.

## API

### NewDispatcher() - Create a new Dispatcher instance

```
NewDispatcher(opts... DispatcherOption)

  * @param {string} connectUrl - Kubernetes service url
  * @param {DispatcherOption} opts - Configuration options for dispatcher instance
  * @returns {*Dispatcher, error} - Pointer to new dispatcher instance, nil if error

Example:

  dispatcher, err := grpcdispatcher.NewDispatcher(
    "kubernetes://my-service.my-namespace",
    grpcdispatcher.WithWithDialOptions(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
  )
```

### Dispatcher - The Dispatcher class

```
Dispatcher

  Fanout(ctx context.Context, fn DispatchHandler)

    * @param {context.Context} ctx - Parent object for child contexts passed to dispatch handler
    * @param {DispatchHandler} fn - The function to be called for each available gRPC server

    Example 1:

      dispatcher, err := grpcdispatcher.NewDispatcher(
        "kubernetes://my-service.my-namespace",
        grpcdispatcher.WithWithDialOptions(
			    grpc.WithTransportCredentials(insecure.NewCredentials()),
		    ),
  )

  rootCtx := context.Background()

  dispatcher.Fanout(rootCtx, func(ctx context.Context, conn *grpc.ClientConn) {
    // make grpc request here
  })


  FanoutSubscribe(ctx context.Context, fn DispatchHandler)

    * @param {context.Context} ctx - Parent object for child contexts passed to dispatch handler
    * @param {DispatchHandler} fn - The function to be called for each available gRPC server
    * @returns {*Subscription, error} - Pointer to new subscription, nil if error

Example 2:

  dispatcher, err := grpcdispatcher.NewDispatcher(
    "kubernetes://my-service.my-namespace",
    grpcdispatcher.WithWithDialOptions(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
  )

  rootCtx, cancel := context.WithCancel(context.Background())

  sub, err := dispatcher.FanoutSubscribe(rootCtx, func(ctx context.Context, conn *grpc.ClientConn) {
    // make grpc request here
  })
  if err != nil {
    return
  }

  // run for 10 seconds then stop listening for new gRPC servers
  time.Sleep(10 * time.Second)

  sub.Unsubscribe()

  // let ongoing queries continue running for 10 seconds
  time.Sleep(10 * time.Second)

  // cancel ongoing requests
  cancel()
```

## Develop

To develop gRPC-Dispatcher, first create a Kubernetes dev cluster using a dev cluster tool that [works with Tilt](https://docs.tilt.dev/choosing_clusters#microk8s). To automate the process you can also use [ctlptl](https://github.com/tilt-dev/ctlptl) and one of the configs available in the [`hack/ctlptl`](hack/ctlptl) directory. For example, to create a dev cluster using [minikube](https://minikube.sigs.k8s.io/docs/) you can use this command:

```console
ctlptl apply -f hack/ctlptl/minikube.yaml
```

Once the dev cluster is running and `kubectl` is pointing to it, you can bring up the dev environment using Tilt. This will create a web app that queries multiple gRPC servers using the code in the [example](example/) directory: 

```console
tilt up
```

Once the web app is running you can access it on port 4000 on your localhost:
[http://localhost:4000](http://localhost:4000)

To teardown the dev environment run these commands:
```console
tilt down
ctlptl delete -f hack/ctlptl/minikube.yaml
```

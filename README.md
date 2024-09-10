# gRPC-Dispatcher

gRPC-Dispatcher is a Go library that facilitates sending queries to multiple gRPC servers running on Kubernetes simultaneously

## Introduction

gRPC-Dispatcher is a Go library that facilitates sending queries to multiple gRPC servers running on Kubernetes simultaneously. It simplifies the process of managing connections to gRPC servers hosted on Kubernetes pods by providing a dispatcher that keeps track of pod IP addresses and sends fanout queries to all servers simultaneously. The dispatcher supports both instantaneous fanout queries (sending queries to all currently available servers) and subscription-based fanout queries (sending queries also as new servers become available).

Currently, the library supports gRPC servers running behind a Kubernetes service and it has the following features:

* Uses Kubernetes API to get pod IPs using highly efficient EndpointSlices
* Connects to new servers eagerly in background so new queries are instantaneous
* Supports long-running streaming queries to ephemeral servers

Try it out and let us know what you think! If you notice any bugs or have any feature requests just create a GitHub Issue.

## Quickstart

First, add the library to your Go project:
```console
go get github.com/kubetail-org/grpc-dispatcher
```

Next write your dispatch code:
```go
import (
  "context"

  "github.com/kubetail-org/grpc-dispatcher"
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

> [!IMPORTANT]
> gRPC-Dispatcher needs `list` and `watch` permisions for the `endpointslices` resource in the Kubernetes API

## Docs

See [Go docs](https://pkg.go.dev/github.com/kubetail-org/grpc-dispatcher) for library documentation.

## Example

You can see an example implementation in the [`example/`](example/) directory. To run the example in a Kubernetes environment see the [Develop](#Develop) section below. 

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

# gRPC-Dispatcher Example Project

Go module containing example project that uses gRPC-Dispatcher

## Dependencies

```console
go install google.golang.org/protobuf/cmd/protoc-gen-go
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
```

## gRPC

To run the gRPC code generator use the `go generate` command:

```console
go generate ./...
```

## Client

To run the example client:

```console
go run ./cmd/client
```

## Server

To run the example server:

```console
go run ./cmd/server
```

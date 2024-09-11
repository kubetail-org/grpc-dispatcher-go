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

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/kubetail-org/grpc-dispatcher-go/example/internal/examplepb"
)

type ExampleService struct {
	examplepb.UnimplementedExampleServiceServer
	serverName string
}

func (svc *ExampleService) Echo(ctx context.Context, req *examplepb.EchoRequest) (*examplepb.EchoResponse, error) {
	if req.DurationMs != 0 {
		time.Sleep(time.Duration(req.DurationMs) * time.Millisecond)
	}

	return &examplepb.EchoResponse{
		ServerName: svc.serverName,
		Message:    req.GetMessage(),
	}, nil
}

func main() {
	// listen for termination signals as early as possible
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer close(quit)

	// get server name
	serverName := os.Getenv("SERVER_NAME")

	// init logger
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", serverName), log.Ldate|log.Ltime)

	// init service
	svc := &ExampleService{serverName: serverName}

	// init server
	server := grpc.NewServer()
	examplepb.RegisterExampleServiceServer(server, svc)

	// init listener
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		logger.Fatal(err)
	}

	// run server in goroutine
	go func() {
		logger.Println("Starting server on :50051")
		if err := server.Serve(lis); err != nil {
			logger.Fatal(err)
		}
	}()

	// wait for termination signal
	<-quit

	logger.Println("Starting graceful shutdown...")

	// graceful shutdown with 30 sec deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		logger.Println("Completed graceful shutdown")
	case <-ctx.Done():
		logger.Println("Exceeded deadline, shutting down forcefully")
		server.Stop()
	}
}

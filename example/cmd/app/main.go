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
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kubetail-org/grpc-dispatcher-go/example/internal/app"
)

func main() {
	// listen for termination signals as early as possible
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer close(quit)

	// create app
	app, err := app.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	// create server
	server := http.Server{
		Addr:         ":4000",
		Handler:      app,
		IdleTimeout:  2 * time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // disabled for SSE
	}

	// run server in goroutine
	go func() {
		log.Println("Starting server on :4000")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// wait for termination signal
	<-quit

	log.Println("Starting graceful shutdown...")

	// graceful shutdown with 30 sec deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// attempt graceful shutdown
	done := make(chan struct{})
	go func() {
		if err := server.Shutdown(ctx); err != nil {
			log.Println(err)
		}
		close(done)
	}()

	// shutdown app
	app.Shutdown()

	select {
	case <-done:
		log.Println("Completed graceful shutdown")
	case <-ctx.Done():
		log.Println("Exceeded deadline, exiting now")
	}
}

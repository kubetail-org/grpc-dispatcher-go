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

package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher"
	"github.com/kubetail-org/grpc-dispatcher/example/internal/examplepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type App struct {
	*gin.Engine
	dispatcher *grpcdispatcher.Dispatcher
}

// Shutdown background processes gracefully
func (app *App) Shutdown() {
	if err := app.dispatcher.Shutdown(); err != nil {
		log.Println(err)
	}
}

func NewApp() (*App, error) {
	// init
	app := &App{Engine: gin.New()}

	// load templates
	basepath, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	app.LoadHTMLGlob(path.Join(basepath, "templates/*"))

	// init grpc dispatcher
	dispatcher, err := grpcdispatcher.NewDispatcher(
		"kubernetes://example-server",
		grpcdispatcher.WithDialOptions(
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		return nil, err
	}
	app.dispatcher = dispatcher

	// start dispatcher
	app.dispatcher.Start()

	// homepage
	app.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	// handle for fanout example
	app.GET("/fanout-events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		rootCtx := c.Request.Context()

		var mu sync.Mutex
		sendMsg := func(msg string) {
			mu.Lock()
			defer mu.Unlock()
			if rootCtx.Err() == nil {
				c.SSEvent("message", msg)
				c.Writer.Flush()
			}
		}

		app.dispatcher.Fanout(rootCtx, func(ctx context.Context, conn *grpc.ClientConn) {
			// init grpc client
			client := examplepb.NewExampleServiceClient(conn)

			// execute grpc request
			resp, err := client.Echo(ctx, &examplepb.EchoRequest{Message: "hello", DurationMs: 0})
			if err != nil {
				log.Printf("Error: %v", err)
				return
			}

			// send response to browser
			sendMsg(fmt.Sprintf("%s: %s\n", resp.ServerName, resp.Message))
		})

		sendMsg("finished")
	})

	// handler for fanout subscribe example
	app.GET("/fanout-subscribe-events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		rootCtx := c.Request.Context()

		var mu sync.Mutex
		sendMsg := func(msg string) {
			mu.Lock()
			defer mu.Unlock()
			if rootCtx.Err() == nil {
				c.SSEvent("message", msg)
				c.Writer.Flush()
			}
		}

		sub, err := app.dispatcher.FanoutSubscribe(rootCtx, func(ctx context.Context, conn *grpc.ClientConn) {
			// init grpc client
			client := examplepb.NewExampleServiceClient(conn)

			// execute grpc request
			resp, err := client.Echo(ctx, &examplepb.EchoRequest{Message: "hello", DurationMs: 0})
			if err != nil {
				log.Printf("Error: %v", err)
				return
			}

			// send response to browser
			sendMsg(fmt.Sprintf("%s: %s\n", resp.ServerName, resp.Message))
		})
		if err != nil {
			sendMsg(err.Error())
			return
		}
		defer sub.Unsubscribe()

		<-c.Writer.CloseNotify()
	})

	// wait until dispatcher is ready to start searving requests
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app.dispatcher.Ready(ctx)

	return app, nil
}

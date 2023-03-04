# Exit Go routines properly with Go Quitter

Golang is a langue known for its simplicity, performance, and concurrency model. Goroutines are one of the most basic units to organize a Go program. A goroutine is a lightweight execution thread that executes concurrently, but not necessarily in parallel, with the rest of the program.

Working with multi-threaded applications can be challenging, especially when it comes to enabling a graceful shutdown mechanism. The goal of a graceful shutdown is to ensure that background threads are not abruptly terminated in the middle of a critical operation, which could lead to data corruption or other issues.

For example, let's consider a Go program that forks 20 goroutines from the main routine, and each of them creates another 20 goroutines, and so on. With so many goroutines running in the background, it becomes crucial to implement a reliable mechanism for shutting down the program gracefully. When the main routine receives a termination signal, it needs to inform all the goroutines running in the background to exit gracefully. This can be achieved using channels and signals, which are core features of the Go programming language.

This post introduces [go-quitter](https://github.com/rodruizronald/go-quitter), a tool designed to send a quit signal to goroutines, allowing them to perform necessary cleanup tasks and save their states. When using go-quitter, the program will wait for a set amount of time before exiting, ensuring that all necessary tasks are completed while avoiding indefinite locks. Additionally, go-quitter can identify routines that fail to return. This feature is particularly useful for debugging and identifying potential issues in the code.

Overall, go-quitter is a useful solution for managing background processes in Go programs and ensuring they gracefully exit when necessary.

Before jumping into the go-quitter, we will cover some of the synchronization primitives that the Golang library provides, such as channels, signals, waiting groups, and error groups, to understand better how these are used when building a graceful shutdown mechanism. A good understating of them is also relevant since the go-quitter is built on top of them.

## Channels and Signals

Channels and signals can be used together to implement a graceful shutdown mechanism. To use channels for a graceful shutdown, you can create a quit channel that is used to signal the program to shut down. All goroutines that need to be shut down gracefully should check the quit channel periodically, and terminate cleanly when the channel is closed. For example, you can create a quit channel like this:

```go
quitChan := make(chan struct{})
```

Then, you can pass the `quitChan` to all the goroutines that need to be shut down gracefully. In each goroutine, you can periodically check the `quitChan` , like this:

```go
for {
    select {
    case <-quitChan:
        // Clean up and return
    default:
        // Continue with normal work
    }
}
```

To use signals for a graceful shutdown, you can handle the `SIGINT` and `SIGTERM` signals using the `os/signal` package. When a signal is received, you can close the `quitChan`  to signal all goroutines to shut down. For example, you can create a `signal.Notify()` function like this:

```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
```

Then, you can wait for a signal using the `sigChan` , and close the quit channel when a signal is received, like this:

```go
go fun() {
	<-sigChan
	close(quitChan)
}
```

By using channels and signals together in this way, you can implement a robust and reliable graceful shutdown mechanism for your concurrent program, ensuring that all goroutines are terminated cleanly and all resources are properly released.

## WaitGroup and ErrorGroup

The `sync.WaitGroup` package is used to synchronize goroutines in Go. It is useful in cases where a program needs to wait for a group of goroutines to complete before continuing. In the context of graceful shutdown, it can be used to wait for all active goroutines to complete before shutting down the application.

Here's an example of how it can be used for graceful shutdown:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	quitChan := make(chan struct{})

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-quitChan:
					fmt.Printf("goroutine %d stopped\n", i)
					return
				default:
					fmt.Printf("goroutine %d running\n", i)
					time.Sleep(time.Second)
				}
			}
		}(i)
	}

	time.Sleep(5 * time.Second)
	close(quitChan)
	wg.Wait()

	fmt.Println("application shutdown gracefully")
}
```

In the above code, we create a `WaitGroup` and `quitChan` channel. We then spawn five goroutines that perform some work inside an infinite loop. The loop continues until the `quitChan` channel is closed. Once the `quitChan` channel is closed, the goroutines exit and the `WaitGroup` is notified with each call to `wg.Done()`. Finally, we wait for all the goroutines to complete by calling `wg.Wait()`.

The `errgroup.Group` package is used to handle errors in a group of goroutines. It is similar to `WaitGroup`, but it also handles errors that occur in any of the goroutines. In the context of graceful shutdown, it can be used to wait for all active goroutines to complete, while also handling any errors that occur in those goroutines.

Here's an example of how it can be used for graceful shutdown:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	mainCtx, cancel := context.WithTimeout(context.Background(), time.Duration(5*time.Second))
	defer cancel()

	eg, egCtx := errgroup.WithContext(mainCtx)

	for i := 0; i < 5; i++ {
		id := i
		eg.Go(func() error {
			for {
				select {
				case <-egCtx.Done():
					fmt.Printf("goroutine %d stopped\n", id)
					return egCtx.Err()
				default:
					fmt.Printf("goroutine %d running\n", id)
					time.Sleep(time.Second)
				}
			}
		})
	}

	if err := eg.Wait(); err != nil {
		fmt.Printf("application shutdown with error: %s\n", err.Error())
	} else {
		fmt.Println("application shutdown gracefully")
	}
}
```

In the above code, we create an `errgroup.Group` with a timeout context to force an exit. We then spawn five goroutines that perform some work inside an infinite loop. The loop continues until the `egCtx` context is canceled. Once the `egCtx` context is canceled, the goroutines exit and the `errgroup.Group` is notified. Finally, we wait for all the goroutines to complete by calling `eg.Wait()` and check if there were any errors.

## Context

In Go, a `context` is a mechanism that is used to manage cancellation signals and deadlines in long-running operations. It is a standard way to propagate cancellation signals and deadlines across API boundaries and can be used to facilitate graceful shutdown of applications.

Here's an example of how `context` can be used for graceful shutdown:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Duration(5*time.Second))
	defer cancel()

	mainCtx, stop := signal.NotifyContext(timeoutCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go runOperation(mainCtx)

	<-mainCtx.Done()

	fmt.Println("graceful shutdown")
}

func runOperation(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			fmt.Println("context cancelled, stopping operation")
			return
		default:
			fmt.Println("running operation...")
			time.Sleep(time.Second)
		}
	}
}
```

In the above code, we create a main context `mainCtx` that listens to OS signals and cancels the context on interrupting or terminating signals, or parent context cancellation. We then start a long-running operation inside a goroutine that can be canceled using the `mainCtx`. The operation runs inside an infinite loop until the `ctx` is canceled. Finally, we wait for the context to be canceled using `<-ctx.Done()` in the `runOperation` routine and `<-mainCtx.Done()` in the main routine.

Using a context is recommended for handling graceful shutdown in Go applications as it allows for easy propagation of cancellation signals and deadlines across API boundaries. This helps to ensure that resources are cleaned up properly and that the application shuts down gracefully.

## HTTP server graceful shutdown with Signal, Context and ErrGroup

If you want to build an HTTP server that shuts down gracefully based on what we have seen so far, how would you do it? To the best of my knowledge, I have written a short example. I will only show the main routine in this post and highlight the key points, but if you would like to see the full code, please go my [GitHub](https://github.com/rodruizronald/go-quitter/tree/main/example/errgroup).

The implementation for a graceful shutdown relies mainly on two packages: `signal` and `errgroup`. In the main routine, `signal.NotifyContext()` creates a main context that gets closed upon receiving a system termination signal. With this context, the error group is created, and two goroutines are forked to start and stop the HTTP server.

To ensure a graceful shutdown, `srv.Stop()` only gets called when the derived context `egCtx` is closed by either a goroutine returning a non-nil error or if the main context gets closed. This approach allows the server to finish processing any ongoing requests before shutting down.

Finally, `eg.Wait()` waits indefinitely until all goroutines in the error group return. This ensures that all goroutines are gracefully shutdown before the program exits.

```go
func main() {
	mainCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := NewService("8080", mainCtx)

	eg, egCtx := errgroup.WithContext(mainCtx)
	eg.Go(func() error {
		return srv.Start()
	})

	eg.Go(func() error {
		<-egCtx.Done()
		return srv.Stop()
	})

	if err := eg.Wait(); err != nil {
		fmt.Printf("exit reason: %s \n", err)
	}
}
```

## HTTP server graceful shutdown with Go Quitter

Let's take a closer look at the `go-quitter` package and how it works by also implementing an HTTP server that shuts down gracefully. For the full implementation please go to my [GitHub](https://github.com/rodruizronald/go-quitter/tree/main/example/http_server).

To create the main quitter, two parameters are required:

- `quitTimeout`: The amount of time to wait for all background routines to return.
- `quitChans`: A list of channels to listen for quit events.

In addition to the main quitter, `quitter.NewMainQuitter()` returns an exit function that should be called at the end of the `main` routine. If we examine the `exitFunc()` in more detail, we can see that it returns three parameters:

- `exitCode`: This represents the program's exit code. If all routines return successfully and there is no timeout, the value is `0`. If the quitter timeouts while waiting for all routines to return, the value is `1`.
- `selectedChanIdx`: This is the index of `quitChans` that indicates the channel that received data.
- `timeouts`: If `exitCode` is equal to `1`, this slice provides information on which goroutines forked by the main quitter failed to return before the timeout.

Essentially, the `exitFunc()` listens to the given quit channels and holds the program execution. When one of the channels receives data, a quit signal is propagated to all background routines so that they return.

```go
func main() {
	mainQuitter, exit, chans := initMainQuitter()
  defer exit()

	srv := NewService("8080", chans, mainQuitter.Context())

	// If quitter has already quit, a new goroutine cannot be added,
	// so .Stop() is registered first in cases .Start() cannot be added.
	if !mainQuitter.AddGoRoutine(srv.Stop) {
		return
	}

	if !mainQuitter.AddGoRoutine(srv.Start) {
		return
	}
}

func initMainQuitter() (*quitter.Quitter, func(), []interface{}) {
	signalChan := make(chan os.Signal, 1)
	serverErrChan := make(chan error, 1)

	// Listen for OS interrupt and termination signals.
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// List of channels to listen for quit.
	quitChans := []interface{}{signalChan, serverErrChan}

	// For logging purpose, map of quit channels with a description.
	chansMap := make(map[int]string, len(quitChans))
	chansMap[InterruptChanIdx] = "OS interrupt/termination signal"
	chansMap[ServerErrChanIdx] = "Http server error"

	// Must use main quitter in the main goroutine.
	mainQuitter, exitFunc := quitter.NewMainQuitter(quitTimeout, quitChans)

	exitMain := func() {
		exitCode, selectedChanIdx, timeouts := exitFunc()
		fmt.Printf("Received quit from channel '%s'\n", chansMap[selectedChanIdx])

		switch exitCode {
		case 0:
			fmt.Println("Sucesfully quit application, all forked goroutines returned")
		case 1:
			fmt.Println("Failed to quit application, not all forked goroutines returned")
			for _, t := range timeouts {
				fmt.Printf("Timeout waiting done on quitter '%s' due to the following goroutines:\n", t.QuitterName)
				for _, gr := range t.GoRoutines {
					fmt.Printf("\t-  %s\n", gr)
				}
				fmt.Printf("\n")
			}
		default:
			fmt.Println("Quitter exit with unknown code", exitCode)
		}

		os.Exit(exitCode)
	}

	return mainQuitter, exitMain, quitChans
}
```

## Go Quitter vs ErrGroup






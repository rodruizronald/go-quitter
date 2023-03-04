package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	shutdownTimeout = time.Duration(1 * time.Second)
)

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

type APIService struct {
	http.Server
}

func NewService(port string, ctx context.Context) *APIService {
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 5; i++ {
			select {
			case <-r.Context().Done():
				w.Write([]byte("Server shutting down!"))
				return
			case <-time.After(1 * time.Second):
			}
		}

		w.Write([]byte("Hello World!"))
	}))

	return &APIService{
		Server: http.Server{
			Addr:    ":" + port,
			Handler: mux,
			BaseContext: func(_ net.Listener) context.Context {
				return ctx
			},
		},
	}
}

func (s *APIService) Start() error {
	fmt.Println("Starting service")
	return s.ListenAndServe()
}

func (s *APIService) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	fmt.Println("Stopping service")

	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Println("Failed to shutdown server gracefully", err)
		if err = s.Close(); err != nil {
			fmt.Println("Failed to close server", err)
		}
		return err
	}

	fmt.Println("Stopped service successfully")
	return nil
}

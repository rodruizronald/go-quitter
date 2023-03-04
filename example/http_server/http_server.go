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

	"github.com/rodruizronald/go-quitter"
)

const (
	quitTimeout     = time.Duration(3 * time.Second)
	shutdownTimeout = time.Duration(1 * time.Second)
)

const (
	InterruptChanIdx int = iota
	ServerErrChanIdx
)

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

type APIService struct {
	http.Server
	errChan chan error
}

func NewService(port string, chans []interface{}, ctx context.Context) *APIService {
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
		errChan: chans[ServerErrChanIdx].(chan error),
	}
}

func (s *APIService) Start(parentQuitter quitter.GoRoutineQuitter) {
	fmt.Println("Starting service")
	s.errChan <- s.ListenAndServe()
}

func (s *APIService) Stop(parentQuitter quitter.GoRoutineQuitter) {
	// Wait for quitter to quit.
	<-parentQuitter.QuitChan()

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	fmt.Println("Stopping service")

	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Println("Failed to shutdown server gracefully", err)
		if err := s.Close(); err != nil {
			fmt.Println("Failed to close server", err)
		}
		return
	}

	fmt.Println("Stopped service successfully")
}

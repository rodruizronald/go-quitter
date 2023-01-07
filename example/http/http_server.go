package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/rodruizronald/go-quitter"
)

const (
	quitTimeout     = time.Duration(2 * time.Second)
	shutdownTimeout = time.Duration(1 * time.Second)
)

const (
	InterruptChanIdx int = iota
	ServerErrChanIdx
)

func main() {
	q, exit, chans := initMainQuitter()
	srv := NewService("8080", chans[ServerErrChanIdx].(chan error))

	// If quitter has already quit, a new goroutine cannot be added,
	// so .Stop() is registered first in cases .Start() cannot be added
	if ok := q.AddGoRoutine(srv.Stop); !ok {
		exit()
	}

	if ok := q.AddGoRoutine(srv.Start); !ok {
		exit()
	}

	exit()
}

func initMainQuitter() (*quitter.Quitter, func(), []interface{}) {
	signalChan := make(chan os.Signal, 1)
	serverErrChan := make(chan error, 1)

	// Listen for OS interrupt signals
	signal.Notify(signalChan, os.Interrupt)

	// List of channels to listen for quit
	quitChans := []interface{}{signalChan, serverErrChan}

	// For logging purpose, map of quit channels with a description
	chansMap := make(map[int]string, len(quitChans))
	chansMap[InterruptChanIdx] = "OS interrupt signal"
	chansMap[ServerErrChanIdx] = "Http server error"

	// Must use main quitter in the main goroutine
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

func NewService(port string, errChan chan error) *APIService {
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	return &APIService{
		Server: http.Server{
			Addr:    ":" + port,
			Handler: mux,
		},
		errChan: errChan,
	}
}

func (s *APIService) Start(q *quitter.Quitter) {
	fmt.Println("Starting service")
	s.errChan <- s.ListenAndServe()
}

func (s *APIService) Stop(q *quitter.Quitter) {
	// Wait for quitter to quit
	<-q.QuitChan()

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

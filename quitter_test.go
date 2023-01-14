package quitter

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

const (
	quitTimeout = time.Duration(10 * time.Millisecond)
	doneTimeout = time.Duration(10 * time.Millisecond)
)

type QuitterSuite struct {
	suite.Suite
	q    *Quitter
	exit ExitFuc
}

func TestQuitterSuite(t *testing.T) {
	suite.Run(t, new(QuitterSuite))
}

func (s *QuitterSuite) SetupTest() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	errorChan := make(chan string, 1)
	stringChan := make(chan string, 1)

	parentStatus = parentStatusUnset
	chs := []interface{}{errorChan, stringChan, signalChan}
	s.q, s.exit = NewMainQuitter(quitTimeout, chs)
}

func (s *QuitterSuite) TestMultipleMainQuitters() {
	defer func() {
		if r := recover(); r == nil {
			s.T().Error("succeeded to add multiple parents")
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	_, _ = NewMainQuitter(quitTimeout, []interface{}{signalChan})
	_, _ = NewMainQuitter(quitTimeout, []interface{}{signalChan})
}

func (s *QuitterSuite) TestOSInterruptSignal() {
	// Trigger termination signals
	syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	// Add one job to make exit fail
	s.Equal(true, s.q.add(1), "add failed before quitting")
	exitCode, selectedChanIdx, timeoutInfo := s.exit()
	s.Equal(1, exitCode, "succedded with quitter timeout")
	s.Equal(2, selectedChanIdx, "unexpected channel selected")
	s.Equal(1, len(timeoutInfo), "unexpected number of timeout quitters")
	s.Equal(MainQuitterName, timeoutInfo[0].QuitterName, "timeout quitter name not matching expected value")
}

func (s *QuitterSuite) TestHasToQuit() {
	s.Equal(false, s.q.HasToQuit(), "invalid state before qutting")
	s.q.SendQuit()
	s.Equal(true, s.q.HasToQuit(), "invalid state after quitting")
}

func (s *QuitterSuite) TestAddBeforeQuit() {
	ok := s.q.add(1)
	s.q.SendQuit()
	s.Equal(true, ok, "add failed before quitting")
}

func (s *QuitterSuite) TestAddAfterQuit() {
	s.q.SendQuit()
	s.Equal(false, s.q.add(1), "add succeeded after quitting")
}

func (s *QuitterSuite) TestDoneWithoutAdd() {
	s.Panics(s.q.done, "done succeeded without adding")
}

func (s *QuitterSuite) TestTooManyDone() {
	s.Equal(true, s.q.add(2), "add failed before quitting")

	s.q.done()
	s.q.done()
	s.Panics(s.q.done, "invalid done succeeded")
}

func (s *QuitterSuite) TestWaitDoneTimeoutTrue() {
	s.Equal(true, s.q.add(1), "add failed before quitting")

	waitTimeout, _ := s.q.WaitDone(doneTimeout)
	s.Equal(true, waitTimeout, "wait succeeded without done")
}

func (s *QuitterSuite) TestWaitDoneTimeoutFalse() {
	s.Equal(true, s.q.add(1), "add failed before quitting")

	s.q.done()
	waitTimeout, _ := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait failed with done")
}

func (s *QuitterSuite) TestNonConcurrentChildsWaitDoneTimeoutTrue() {
	parent := s.q
	s.Equal(true, parent.add(3), "add failed before quitting")
	parent.done()
	parent.done()
	parent.done()

	// 2 level depth
	child1 := NewChildQuitter(parent, "child1")
	s.Equal(true, child1.add(1), "add failed before quitting")
	child1.done()
	child1A := NewChildQuitter(child1, "child1A")
	s.Equal(true, child1A.add(1), "add failed before quitting")
	child1B := NewChildQuitter(child1, "child1B")
	s.Equal(true, child1B.add(1), "add failed before quitting")
	child1B.done()

	// 3 level depth
	child2 := NewChildQuitter(parent, "child2")
	s.Equal(true, child2.add(2), "add failed before quitting")
	child2.done()
	child2A := NewChildQuitter(child2, "child2A")
	s.Equal(true, child2A.add(1), "add failed before quitting")
	child2A.done()
	child2A1 := NewChildQuitter(child2A, "child2A1")
	s.Equal(true, child2A1.add(1), "add failed before quitting")

	// 1 level depth
	child3 := NewChildQuitter(parent, "child3")
	s.Equal(true, child3.add(3), "add failed before quitting")
	child3.done()
	child3.done()

	parent.SendQuit()
	waitTimeout, timeouts := parent.WaitDone(doneTimeout)

	s.Equal(true, waitTimeout, "wait succeeded without all done")
	s.Equal(4, len(timeouts), "unexpected number of timeout quitters")

	// List of quitters with goroutines that didn't return
	qs := []string{"child1A", "child2", "child2A1", "child3"}
	for _, ts := range timeouts {
		s.Contains(qs, ts.QuitterName, "succeeded with timeout quitters")
	}
}

func (s *QuitterSuite) TestNonConcurrentChildsWaitDoneTimeoutFlase() {
	parent := s.q
	s.Equal(true, parent.add(3), "add failed before quitting")
	parent.done()
	parent.done()
	parent.done()

	// 2 level depth
	child1 := NewChildQuitter(parent, "child1")
	s.Equal(true, child1.add(1), "add failed before quitting")
	child1.done()
	child1A := NewChildQuitter(child1, "child1A")
	s.Equal(true, child1A.add(1), "add failed before quitting")
	child1A.done()
	child1B := NewChildQuitter(child1, "child1B")
	s.Equal(true, child1B.add(1), "add failed before quitting")
	child1B.done()

	// 3 level depth
	child2 := NewChildQuitter(parent, "child2")
	s.Equal(true, child2.add(2), "add failed before quitting")
	child2.done()
	child2.done()
	child2A := NewChildQuitter(child2, "child2A")
	s.Equal(true, child2A.add(1), "add failed before quitting")
	child2A.done()
	child2A1 := NewChildQuitter(child2A, "child2A1")
	s.Equal(true, child2A1.add(1), "add failed before quitting")

	// 1 level depth
	child3 := NewChildQuitter(parent, "child3")
	s.Equal(true, child3.add(3), "add failed before quitting")
	child3.done()
	child3.done()
	child3.done()

	parent.SendQuit()
	child2A1.done()
	waitTimeout, _ := parent.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait failed with all done")
}

func (s *QuitterSuite) TestConcurrentChilds() {
	parent := s.q
	s.Equal(true, parent.add(1), "add failed before quitting")

	readyToContinue := make(chan struct{}, 2)

	// Parent Routine
	go func(parent *Quitter) {
		defer parent.done()

		child1 := NewChildQuitter(parent, "child1")
		s.Equal(true, child1.add(1), "add failed before quitting")

		// Child Routine 1
		go func(parent *Quitter) {
			defer parent.done()

			child1A := NewChildQuitter(parent, "child1A")
			s.Equal(true, child1A.add(2), "add failed before quitting")

			// Child Routine 1A --> 1
			go func(parent *Quitter) {
				defer parent.done()
				<-parent.QuitChan()
			}(child1A)

			// Child Routine 1A --> 2
			go func(parent *Quitter) {
				defer parent.done()

				child1A2 := NewChildQuitter(parent, "child1A2")
				s.Equal(true, child1A2.add(2), "add failed before quitting")

				// Child Routine 1A2 --> A
				go func(parent *Quitter) {
					defer parent.done()

					for {
						if parent.HasToQuit() {
							break
						}
						time.Sleep(1 * time.Millisecond)
					}
				}(child1A2)

				// Child Routine 1A2 --> B
				go func(parent *Quitter) {
					defer parent.done()

					for {
						if parent.HasToQuit() {
							break
						}
						time.Sleep(1 * time.Millisecond)
					}
				}(child1A2)

				// Do some work
				for i := 0; i < 10; i++ {
					time.Sleep(1 * time.Millisecond)
				}

				child1A2.SendQuit()
				waitTimeout, _ := child1A2.WaitDone(doneTimeout)
				s.Equal(false, waitTimeout, "wait done failed with multiple routines")
				readyToContinue <- struct{}{}

				<-parent.QuitChan()
			}(child1A)

			<-parent.QuitChan()
		}(child1)

		child2 := NewChildQuitter(parent, "child2")
		s.Equal(true, child2.add(1), "add failed before quitting")

		// Child Routine 2
		go func(parent *Quitter) {
			defer parent.done()

			child2A := NewChildQuitter(parent, "child2A")
			s.Equal(true, child2A.add(2), "add failed before quitting")

			// Child Routine 2A --> 1
			go func(parent *Quitter) {
				defer parent.done()
				for {
					if parent.HasToQuit() {
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
			}(child2A)

			// Child Routine 2A --> 2
			go func(parent *Quitter) {
				defer parent.done()
				for {
					if parent.HasToQuit() {
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
			}(child2A)

			// Do some work
			for i := 0; i < 10; i++ {
				time.Sleep(1 * time.Millisecond)
			}

			child2A.SendQuit()
			waitTimeout, _ := child2A.WaitDone(doneTimeout)
			s.Equal(false, waitTimeout, "wait done failed with multiple routines")
			readyToContinue <- struct{}{}

			<-parent.QuitChan()
		}(child2)

		<-parent.QuitChan()
	}(parent)

	// Wait for all routines to be running and internal testing to be executed
	<-readyToContinue
	<-readyToContinue

	parent.SendQuit()
	waitTimeout, _ := parent.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait done failed with multiple routines")
}

func (s *QuitterSuite) TestSpawn() {
	var started, ended uint32

	spawnerStarted := make(chan struct{})
	s.Equal(true, s.q.add(1), "add failed before quitting")

	// Spawner go-routine
	go func(parent *Quitter) {
		defer parent.done()

		// Tell main routine that spawner is ready
		close(spawnerStarted)

		for {
			time.Sleep(10 * time.Microsecond)
			// Try to add a new routine. If false, it's time to quit
			if !parent.add(1) {
				break
			}

			// Spawn another go routine
			go func() {
				defer func() {
					atomic.AddUint32(&ended, 1)
					parent.done()
				}()
				atomic.AddUint32(&started, 1)
				<-parent.QuitChan()
			}()
		}
	}(s.q)

	// Wait for the spawner to have started
	<-spawnerStarted

	// Wait for the spawner to start some go routines
	time.Sleep(10 * time.Millisecond)

	s.q.SendQuit()
	waitTimeout, _ := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "timeout while waiting for done")

	startedCopy := atomic.LoadUint32(&started)
	endedCopy := atomic.LoadUint32(&ended)
	s.Equal(startedCopy, endedCopy, "number of started/ended go routines don't match")
}

func goodGoRoutine(q GoRoutineQuitter) {
	for {
		if q.HasToQuit() {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func (s *QuitterSuite) TestGoRoutineWaitDoneTimeoutFalse() {
	s.Equal(true, s.q.AddGoRoutine(goodGoRoutine), "add routine failed before quitting")
	s.q.SendQuit()

	waitTimeout, _ := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait done failed with valid go-routine")
	s.Equal(false, s.q.AddGoRoutine(goodGoRoutine), "add routine succeeded after quitting")
}

func badGoRoutine(q GoRoutineQuitter) {
	for {
		time.Sleep(1 * time.Millisecond)
	}
}

func (s *QuitterSuite) TestGoRoutineWaitDoneTimeoutTrue() {
	s.Equal(true, s.q.AddGoRoutine(badGoRoutine), "add routine failed before quitting")
	s.Equal(true, s.q.AddGoRoutine(goodGoRoutine), "add routine failed before quitting")
	s.q.SendQuit()

	waitTimeout, timeouts := s.q.WaitDone(doneTimeout)
	s.Equal(true, waitTimeout, "wait done succeeded with invalid go-routines")
	s.Equal("main", timeouts[0].QuitterName, "badGoRoutine", "timeout with unexpected quitter")
	s.Equal(1, len(timeouts[0].GoRoutines), "unexpected number of go-routines timeout")
	s.Equal(true, strings.Contains(timeouts[0].GoRoutines[0], "badGoRoutine"), "timeout with unexpected go-routine")
	s.Equal(false, s.q.AddGoRoutine(badGoRoutine), "add routine succeeded after quitting")
}

func (s *QuitterSuite) TestQuitterContext() {
	requestReceived := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)

		// Do some work
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Millisecond)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	s.Equal(true, s.q.add(1), "add failed before quitting")
	go func(q *Quitter) {
		defer q.done()

		ctx, cancel := context.WithTimeout(q.Context(), 100*time.Millisecond)
		defer cancel()

		url := server.URL + "/"
		req, err := http.NewRequestWithContext(ctx, "GET", url, bytes.NewReader([]byte{}))
		s.NoError(err)

		_, err = server.Client().Do(req)
		s.Equal(true, errors.Is(err, ErrQuitContext), "unexpected context error received")
	}(s.q)

	// Wait for the server to receive the request
	<-requestReceived

	s.q.SendQuit()
	waitTimeout, _ := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait done with context timeout")
}

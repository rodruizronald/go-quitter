package quitter

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

const (
	quitTimeout = time.Duration(1 * time.Second)
)

type QuitterSuite struct {
	suite.Suite
	q *Quitter
}

func TestQuitterSuite(t *testing.T) {
	suite.Run(t, new(QuitterSuite))
}

func (s *QuitterSuite) SetupTest() {
	parentStatus = parentStatusUnset
	s.q = NewParentQuitter(quitTimeout)
}

func (s *QuitterSuite) TestMultipleParents() {
	defer func() {
		if r := recover(); r == nil {
			s.T().Error("succeeded to add multiple parents")
		}
	}()

	_ = NewParentQuitter(quitTimeout)
	_ = NewParentQuitter(quitTimeout)
}

func (s *QuitterSuite) TestHasToQuit() {
	s.Equal(false, s.q.HasToQuit(), "invalid state before qutting")
	s.q.SendQuit()
	s.Equal(true, s.q.HasToQuit(), "invalid state after quitting")
}

func (s *QuitterSuite) TestAddBeforeQuit() {
	ok := s.q.Add(1)
	s.q.SendQuit()
	s.Equal(true, ok, "add failed before quitting")
}

func (s *QuitterSuite) TestAddAfterQuit() {
	s.q.SendQuit()
	ok := s.q.Add(1)
	s.Equal(false, ok, "add succeeded after quitting")
}

func (s *QuitterSuite) TestDoneWithoutAdd() {
	s.Panics(s.q.Done, "done succeeded without adding")
}

func (s *QuitterSuite) TestTooManyDone() {
	ok := s.q.Add(2)
	s.Equal(true, ok, "add failed before quitting")

	s.q.Done()
	s.q.Done()
	s.Panics(s.q.Done, "invalid done succeeded")
}

func (s *QuitterSuite) TestWaitDoneTimeout() {
	ok := s.q.Add(1)
	s.Equal(true, ok, "add failed before quitting")

	s.Equal(true, s.q.WaitDone(1*time.Millisecond), "wait succeeded without done")
	s.q.Done()
	s.Equal(false, s.q.WaitDone(1*time.Millisecond), "wait failed with done")
}

func (s *QuitterSuite) TestMultipleChildsNonConcurrent() {
	parent := s.q
	s.Equal(true, parent.Add(3), "add failed before quitting")
	parent.Done()
	parent.Done()
	parent.Done()

	// 2 level depth
	child1 := NewChildQuitter(parent)
	s.Equal(true, child1.Add(1), "add failed before quitting")
	child1.Done()
	child1A := NewChildQuitter(child1)
	s.Equal(true, child1A.Add(1), "add failed before quitting")
	child1A.Done()
	child1B := NewChildQuitter(child1)
	s.Equal(true, child1B.Add(1), "add failed before quitting")
	child1B.Done()

	// 3 level depth
	child2 := NewChildQuitter(parent)
	s.Equal(true, child2.Add(2), "add failed before quitting")
	child2.Done()
	child2.Done()
	child2A := NewChildQuitter(child2)
	s.Equal(true, child2A.Add(1), "add failed before quitting")
	child2A.Done()
	child2A1 := NewChildQuitter(child2A)
	s.Equal(true, child2A1.Add(1), "add failed before quitting")

	// 1 level depth
	child3 := NewChildQuitter(parent)
	s.Equal(true, child3.Add(3), "add failed before quitting")
	child3.Done()
	child3.Done()
	child3.Done()

	parent.SendQuit()
	s.Equal(true, parent.WaitDone(1*time.Millisecond), "wait succeeded without all done")
	child2A1.Done()
	s.Equal(false, parent.WaitDone(1*time.Millisecond), "wait failed with all done")
}

func (s *QuitterSuite) TestMultipleChildsConcurrent() {
	parent := s.q
	s.Equal(true, parent.Add(1), "add failed before quitting")

	readyToContinue := make(chan struct{})

	// Parent Routine
	go func(parent *Quitter) {
		defer parent.Done()

		child1 := NewChildQuitter(parent)
		s.Equal(true, child1.Add(1), "add failed before quitting")

		// Child Routine 1
		go func(parent *Quitter) {
			defer parent.Done()

			child1A := NewChildQuitter(parent)
			s.Equal(true, child1A.Add(2), "add failed before quitting")

			// Child Routine 1A --> 1
			go func(parent *Quitter) {
				defer parent.Done()
				<-parent.QuitChan()
			}(child1A)

			// Child Routine 1A --> 2
			go func(parent *Quitter) {
				defer parent.Done()

				child1A2 := NewChildQuitter(parent)
				s.Equal(true, child1A2.Add(2), "add failed before quitting")

				// Child Routine 1A2 --> A
				go func(parent *Quitter) {
					defer parent.Done()

					for {
						if parent.HasToQuit() {
							break
						}
						time.Sleep(1 * time.Millisecond)
					}
				}(child1A2)

				// Child Routine 1A2 --> B
				go func(parent *Quitter) {
					defer parent.Done()

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
				s.Equal(false, child1A2.WaitDone(10*time.Millisecond), "wait done failed with multiple routines")

				readyToContinue <- struct{}{}

				<-parent.QuitChan()
			}(child1A)

			<-parent.QuitChan()
		}(child1)

		child2 := NewChildQuitter(parent)
		s.Equal(true, child2.Add(1), "add failed before quitting")

		// Child Routine 2
		go func(parent *Quitter) {
			defer parent.Done()

			child2A := NewChildQuitter(parent)
			s.Equal(true, child2A.Add(2), "add failed before quitting")

			// Child Routine 2A --> 1
			go func(parent *Quitter) {
				defer parent.Done()
				for {
					if parent.HasToQuit() {
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
			}(child2A)

			// Child Routine 2A --> 2
			go func(parent *Quitter) {
				defer parent.Done()
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
			s.Equal(false, child2A.WaitDone(10*time.Millisecond), "wait done failed with multiple routines")

			readyToContinue <- struct{}{}

			<-parent.QuitChan()
		}(child2)

		<-parent.QuitChan()
	}(parent)

	// Wait for all routines to be running and internal testing to be executed
	<-readyToContinue
	<-readyToContinue

	parent.SendQuit()
	s.Equal(false, parent.WaitDone(10*time.Millisecond), "wait done failed with multiple routines")
}

func (s *QuitterSuite) TestSpawn() {
	var started, ended uint32

	spawnerStarted := make(chan struct{})
	s.Equal(true, s.q.Add(1), "add failed before quitting")

	// Spawner go-routine
	go func(parent *Quitter) {
		defer parent.Done()

		// Tell main routine that spawner is ready
		close(spawnerStarted)

		for {
			time.Sleep(10 * time.Microsecond)
			// Try to add a new routine. If false, it's time to quit
			if !parent.Add(1) {
				break
			}

			// Spawn another go routine
			go func() {
				defer func() {
					parent.Done()
					atomic.AddUint32(&ended, 1)
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
	s.Equal(false, s.q.WaitDone(10*time.Millisecond), "timeout while waiting for done")
	startedCopy := atomic.LoadUint32(&started)
	endedCopy := atomic.LoadUint32(&ended)
	s.Equal(startedCopy, endedCopy, "number of started/ended go routines don't match")
}

func (s *QuitterSuite) TestAddValidRoutine() {
	goodRoutine := func(q *Quitter) {
		for {
			if q.HasToQuit() {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
	}

	s.Equal(true, s.q.AddRoutine(goodRoutine), "add routine failed before quitting")
	s.q.SendQuit()
	s.Equal(false, s.q.WaitDone(10*time.Millisecond), "wait done failed with valid go-routine")
	s.Equal(false, s.q.AddRoutine(goodRoutine), "add routine succeeded after quitting")
}

func (s *QuitterSuite) TestAddInvalidRoutine() {
	badRoutine := func(q *Quitter) {
		for {
			time.Sleep(1 * time.Millisecond)
		}
	}

	s.Equal(true, s.q.AddRoutine(badRoutine), "add routine failed before quitting")
	s.q.SendQuit()
	s.Equal(true, s.q.WaitDone(10*time.Millisecond), "wait done succeeded with invalid go-routines")
	s.Equal(false, s.q.AddRoutine(badRoutine), "add routine succeeded after quitting")
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

	s.Equal(true, s.q.Add(1), "add failed before quitting")
	go func(q *Quitter) {
		defer q.Done()

		ctx, cancel := context.WithTimeout(q.Context(), 100*time.Millisecond)
		defer cancel()

		url := server.URL + "/"
		req, err := http.NewRequestWithContext(ctx, "GET", url, bytes.NewReader([]byte{}))
		s.NoError(err)

		_, err = server.Client().Do(req)
		s.Equal(true, errors.Is(err, ErrContextQuitted), "unexpected context error received")
	}(s.q)

	// Wait for the server to receive the request
	<-requestReceived

	s.q.SendQuit()
	s.Equal(false, s.q.WaitDone(10*time.Millisecond), "wait done with context timeout")
}

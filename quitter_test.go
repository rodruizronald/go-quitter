package quitter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func (s *QuitterSuite) TestCreateMultipleMainQuitters() {
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

func (s *QuitterSuite) TestAddBeforeQuit() {
	s.Equal(true, s.q.add(1), "add failed before quitting")
	s.q.SendQuit()
}

func (s *QuitterSuite) TestAddAfterQuit() {
	s.q.SendQuit()
	s.Equal(false, s.q.add(1), "add succeeded after quitting")
}

func (s *QuitterSuite) TestDoneWithoutAdd() {
	s.Panics(s.q.done, "done succeeded without adding")
}

func (s *QuitterSuite) TestDoneWithAdd() {
	s.Equal(true, s.q.add(1), "add failed before quitting")
	s.q.done()
}

func (s *QuitterSuite) TestMoreDoneThanAdd() {
	s.Equal(true, s.q.add(2), "add failed before quitting")
	s.q.done()
	s.q.done()
	s.Panics(s.q.done, "invalid done succeeded")
}

func (s *QuitterSuite) TestHasToQuit() {
	s.Equal(false, s.q.HasToQuit(), "invalid state before qutting")
	s.q.SendQuit()
	s.Equal(true, s.q.HasToQuit(), "invalid state after quitting")
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

func (s *QuitterSuite) TestExitFuncWithInterruptSignal() {
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

func dummyGoRoutine(q GoRoutineQuitter) {}

func (s *QuitterSuite) TestAddGoRoutineBeforeQuit() {
	s.Equal(true, s.q.AddGoRoutine(dummyGoRoutine), "add goroutine failed before quitting")
	s.q.SendQuit()
}

func (s *QuitterSuite) TestAddGoRoutineAfterQuit() {
	s.q.SendQuit()
	s.Equal(false, s.q.AddGoRoutine(dummyGoRoutine), "add goroutine succeeded after quitting")
}

func goodGoRoutine(q GoRoutineQuitter) {
	for !q.HasToQuit() {
		time.Sleep(1 * time.Millisecond)
	}
}

func (s *QuitterSuite) TestAddGoRoutineWaitDoneTimeoutFalse() {
	s.Equal(true, s.q.AddGoRoutine(goodGoRoutine), "add routine failed before quitting")
	s.q.SendQuit()

	waitTimeout, timeouts := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait done failed with valid goroutine")
	s.Equal(0, len(timeouts), "unexpected number of timeout quitters")
}

func badGoRoutine(q GoRoutineQuitter) {
	for {
		time.Sleep(1 * time.Millisecond)
	}
}

func (s *QuitterSuite) TestAddGoRoutineWaitDoneTimeoutTrue() {
	s.Equal(true, s.q.AddGoRoutine(badGoRoutine), "add routine failed before quitting")
	s.q.SendQuit()

	waitTimeout, timeouts := s.q.WaitDone(doneTimeout)
	s.Equal(true, waitTimeout, "wait done succeeded with invalid go-routines")
	s.Equal(1, len(timeouts), "unexpected number of timeout quitters")
	if len(timeouts) > 0 {
		s.Equal("main", timeouts[0].QuitterName, "timeout with unexpected quitter")
		s.Equal(1, len(timeouts[0].GoRoutines), "unexpected number of goroutines timeout")
		s.Equal(true, strings.Contains(timeouts[0].GoRoutines[0], "badGoRoutine"), "timeout with unexpected goroutine")
	}
}

func (s *QuitterSuite) TestNonConcurrentChildsWaitDoneTimeoutTrue() {
	parent := s.q
	s.Equal(true, parent.add(3), "add failed before quitting")
	parent.done()
	parent.done()
	parent.done() // parent - Add three, mark three as done

	child1 := NewChildQuitter(parent, "child1")
	s.Equal(true, child1.add(1), "add failed before quitting")
	child1A := NewChildQuitter(child1, "child1A")
	s.Equal(true, child1A.add(1), "add failed before quitting")
	child1B := NewChildQuitter(child1, "child1B")
	s.Equal(true, child1B.add(1), "add failed before quitting")
	child1.done() // child1 - Add one, mark one as done
	// child1A - Add one, mark zero as done
	child1B.done() // child1B - Add one, mark one as done

	child2 := NewChildQuitter(parent, "child2")
	s.Equal(true, child2.add(2), "add failed before quitting")
	child2A := NewChildQuitter(child2, "child2A")
	s.Equal(true, child2A.add(1), "add failed before quitting")
	child2A1 := NewChildQuitter(child2A, "child2A1")
	s.Equal(true, child2A1.add(1), "add failed before quitting")
	child2.done()  // child2 - Add two, mark one as done
	child2A.done() // child2A - Add one, mark one as done
	// child2A1 - Add one, mark zero as done

	child3 := NewChildQuitter(parent, "child3")
	s.Equal(true, child3.add(3), "add failed before quitting")
	child3.done()
	child3.done() // child3 - Add three, mark two as done

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
	parent.done() // parent - Add three, mark three as done

	child1 := NewChildQuitter(parent, "child1")
	s.Equal(true, child1.add(1), "add failed before quitting")
	child1A := NewChildQuitter(child1, "child1A")
	s.Equal(true, child1A.add(1), "add failed before quitting")
	child1B := NewChildQuitter(child1, "child1B")
	s.Equal(true, child1B.add(1), "add failed before quitting")
	child1.done()
	child1A.done() // child1A - Add one, mark one as done
	child1B.done() // child1B - Add one, mark one as done

	child2 := NewChildQuitter(parent, "child2")
	s.Equal(true, child2.add(2), "add failed before quitting")
	child2A := NewChildQuitter(child2, "child2A")
	s.Equal(true, child2A.add(1), "add failed before quitting")
	child2A1 := NewChildQuitter(child2A, "child2A1")
	s.Equal(true, child2A1.add(1), "add failed before quitting")
	child2.done()
	child2.done()   // child2 - Add two, mark two as done
	child2A.done()  // child2A - Add one, mark one as done
	child2A1.done() // child2A1 - Add one, mark one as done

	child3 := NewChildQuitter(parent, "child3")
	s.Equal(true, child3.add(3), "add failed before quitting")
	child3.done()
	child3.done()
	child3.done() // child3 - Add three, mark three as done

	parent.SendQuit()
	waitTimeout, timeouts := parent.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait failed with all done")
	s.Equal(0, len(timeouts), "unexpected number of timeout quitters")
}

type childRoutine struct {
	suite.Suite
	childCounter int
	childName    string
	readyFlag    chan struct{}
}

func (r *childRoutine) Run(parentQuitter GoRoutineQuitter) {
	childQuitter := NewChildQuitter(parentQuitter, r.childName)

	if numberChilds := (r.childCounter + 1); numberChilds <= 3 {
		childName := fmt.Sprintf("%s-%d", r.childName, numberChilds)
		newChild := childRoutine{r.Suite, numberChilds, childName, r.readyFlag}
		r.Suite.Equal(true, childQuitter.AddGoRoutine(newChild.Run), fmt.Sprintf("add %s failed before quitting", childName))
	} else {
		// Tell main routine all goroutines are running
		r.readyFlag <- struct{}{}
	}

	<-parentQuitter.QuitChan()
}

func (s *QuitterSuite) TestConcurrentChildsWithAddGoRoutine() {
	parent := s.q
	readyFlag := make(chan struct{}, 2)

	child1 := childRoutine{s.Suite, 0, "childA", readyFlag}
	s.Equal(true, parent.AddGoRoutine(child1.Run), "add child1 failed before quitting")

	child2 := childRoutine{s.Suite, 0, "childB", readyFlag}
	s.Equal(true, parent.AddGoRoutine(child2.Run), "add child2 failed before quitting")

	// Wait for all goroutines to be running
	<-readyFlag
	<-readyFlag

	parent.SendQuit()
	waitTimeout, timeouts := parent.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "wait done failed with multiple routines")
	s.Equal(0, len(timeouts), "unexpected number of timeout quitters")
}

func (s *QuitterSuite) TestConcurrentChildsWithoutAddGoRoutine() {
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

					for !parent.HasToQuit() {
						time.Sleep(1 * time.Millisecond)
					}
				}(child1A2)

				// Child Routine 1A2 --> B
				go func(parent *Quitter) {
					defer parent.done()

					for !parent.HasToQuit() {
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
				for !parent.HasToQuit() {
					time.Sleep(1 * time.Millisecond)
				}
			}(child2A)

			// Child Routine 2A --> 2
			go func(parent *Quitter) {
				defer parent.done()
				for !parent.HasToQuit() {
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

type spawner struct {
	suite.Suite
	started   uint32
	ended     uint32
	readyFlag chan struct{}
}

func (s *spawner) Run(parentQuitter GoRoutineQuitter) {
	childQuitter := NewChildQuitter(parentQuitter, "spawner")

	// Tell main routine spawner is ready
	s.readyFlag <- struct{}{}

	for {
		spawnerRoutine := spawnerRoutine{s.started, s.ended}

		// Try to add a new goroutine. If false, it's time to quit
		if !childQuitter.AddGoRoutine(spawnerRoutine.Run) {
			break
		}

		time.Sleep(10 * time.Microsecond)
	}
}

type spawnerRoutine struct {
	started uint32
	ended   uint32
}

func (s *spawnerRoutine) Run(parentQuitter GoRoutineQuitter) {
	atomic.AddUint32(&s.started, 1)
	<-parentQuitter.QuitChan()
	atomic.AddUint32(&s.ended, 1)
}

func (s *QuitterSuite) TestSpawn() {
	parent := s.q
	readyFlag := make(chan struct{})

	spawner := spawner{Suite: s.Suite, readyFlag: readyFlag}
	s.Equal(true, parent.AddGoRoutine(spawner.Run), "add spawner failed before quitting")

	// Wait for the spawner to be ready
	<-readyFlag

	// Wait for the spawner to fork some goroutines
	time.Sleep(10 * time.Millisecond)

	parent.SendQuit()
	waitTimeout, timeouts := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "timeout while waiting for done")
	s.Equal(0, len(timeouts), "unexpected number of timeout quitters")

	startedCopy := atomic.LoadUint32(&spawner.started)
	endedCopy := atomic.LoadUint32(&spawner.ended)
	s.Equal(startedCopy, endedCopy, "number of started/ended go-routines don't match")
}

type clientRoutine struct {
	suite.Suite
	url    string
	client *http.Client
}

func (c *clientRoutine) Run(parentQuitter GoRoutineQuitter) {
	ctx, cancel := context.WithTimeout(parentQuitter.Context(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.url, bytes.NewReader([]byte{}))
	c.Suite.NoError(err)

	_, err = c.client.Do(req)
	c.Suite.Equal(true, errors.Is(err, ErrQuitContext), "unexpected context error received")
}

func (s *QuitterSuite) TestContext() {
	readyFlag := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		readyFlag <- struct{}{}
		// Wait for longer that the quitter timeout to return to force a quit context error
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := clientRoutine{s.Suite, server.URL + "/", server.Client()}
	s.Equal(true, s.q.AddGoRoutine(client.Run), "add client failed before quitting")

	// Wait for the server to receive the request
	<-readyFlag

	s.q.SendQuit()
	waitTimeout, timeouts := s.q.WaitDone(doneTimeout)
	s.Equal(false, waitTimeout, "timeout while waiting for done")
	s.Equal(0, len(timeouts), "unexpected number of timeout quitters")
}

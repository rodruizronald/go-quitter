package quitter

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type Quitter struct {
	// Name of the quitter.
	name string
	// Quitter to listen for quit events.
	parent *Quitter
	// Quitters listening to this quitter.
	childs []*Quitter
	// List of child quitters that timeout waiting for done when parent quitter quit.
	timeouts   []timeoutQuitter
	timeoutsMu sync.Mutex
	// Map of forked goroutines that are traced by this quitter.
	grsState   map[string]routineState
	grsStateMu sync.RWMutex
	// Channel to signal quit event.
	quitChan   chan struct{}
	quitChanMu sync.Mutex
	// Channel to signal that all forked goroutines returned.
	doneChan     chan struct{}
	doneChanOnce sync.Once
	// Synchronization primitive to wait for forked goroutines to return.
	wg sync.WaitGroup
}

type timeoutQuitter struct {
	// Name of the quitter that timeout.
	QuitterName string
	// Name of the forked goroutines that failed to return.
	// Only available when forking goroutines with the .AddGoRoutine() method.
	GoRoutines []string
}

type ExitFuc func() (
	// Quitter exit status, 0 (success) or 1 (failed).
	exitStatus int,
	// Index of the chan that triggered quit.
	chanIndex int,
	// Timeout information.
	timeouts []timeoutQuitter)

const (
	// Default name of the parent quitter.
	MainQuitterName string = "main"
)

var (
	parentStatus int32
)

const (
	parentStatusUnset int32 = iota
	parentStatusSet
)

// NewMainQuitter returns the main quitter with an exit function to call before exiting the main() routine.
// There can be only one main quitter, the main() goroutine quitter; therefore, calling this function more
// than once will result in a panic.
func NewMainQuitter(quitTimeout time.Duration, chans []interface{}) (*Quitter, ExitFuc) {
	if len(chans) == 0 {
		panic("quitter: no chans to listen to")
	}

	parent := &Quitter{
		name:     MainQuitterName,
		grsState: make(map[string]routineState),
		quitChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}

	if !atomic.CompareAndSwapInt32(&parentStatus, parentStatusUnset, parentStatusSet) {
		panic("quitter: only one main quitter allowed")
	}

	cases := make([]reflect.SelectCase, len(chans))
	for i, ch := range chans {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}

	exit := func() (int, int, []timeoutQuitter) {
		selectedChanIdx, _, _ := reflect.Select(cases)

		parent.SendQuit()

		if isTimeout, timeoutInfo := parent.WaitDone(quitTimeout); isTimeout {
			return 1, selectedChanIdx, timeoutInfo
		}

		return 0, selectedChanIdx, nil
	}

	return parent, exit
}

// QuitChan returns the channel to listen for quit events.
func (q *Quitter) QuitChan() <-chan struct{} {
	return q.quitChan
}

// HasToQuit returns an exit flag.
func (q *Quitter) HasToQuit() bool {
	q.quitChanMu.Lock()
	defer q.quitChanMu.Unlock()

	select {
	case <-q.quitChan:
		return true
	default:
		return false
	}
}

// SendQuit closes the quit channel to signal an exit.
func (q *Quitter) SendQuit() {
	q.quitChanMu.Lock()
	defer q.quitChanMu.Unlock()

	select {
	case <-q.quitChan:
	default:
		close(q.quitChan)
	}
}

// WaitDone waits for all jobs added to the waiting group to be done before the given timeout.
// It returns a flag indicating if the quitter timeout, and a slice with timeout information.
func (q *Quitter) WaitDone(timeout time.Duration) (bool, []timeoutQuitter) {
	waitTimeout := q.wait(timeout)
	return waitTimeout, q.timeouts
}

func (q *Quitter) add(delta int) bool {
	if q.HasToQuit() {
		return false
	}

	q.wg.Add(delta)
	return true
}

func (q *Quitter) done() {
	q.wg.Done()
}

func (q *Quitter) wait(timeout time.Duration) bool {
	waitBufChan := make(chan bool, len(q.childs))

	q.doneChanOnce.Do(func() {
		go func() {
			for _, child := range q.childs {
				go func(cq *Quitter) {
					waitBufChan <- cq.wait(timeout)
				}(child)
			}

			// Wait for all goroutines to return.
			q.wg.Wait()
			close(q.doneChan)
		}()
	})

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	timerFired := true

	select {
	case <-q.doneChan:
		// All goroutines returned.
		timerFired = false
	case <-timer.C:
		q.appendTimeouts(timeoutQuitter{
			QuitterName: q.name,
			GoRoutines:  q.getTimeoutGoRoutines(),
		})
	}

	// If there are child quitters, wait for all recursive calls to .wait() to return.
	for range q.childs {
		if tf := <-waitBufChan; tf {
			timerFired = tf
		}
	}

	// Pass timeout information to parent quitter, until it reaches main quitter.
	if q.name != MainQuitterName && len(q.timeouts) > 0 {
		q.parent.appendTimeouts(q.timeouts...)
	}

	return timerFired
}

func (q *Quitter) appendTimeouts(ts ...timeoutQuitter) {
	q.timeoutsMu.Lock()
	defer q.timeoutsMu.Unlock()
	q.timeouts = append(q.timeouts, ts...)
}

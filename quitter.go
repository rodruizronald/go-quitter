package quitter

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Quitter struct {
	quitChan      chan struct{}
	doneChan      chan struct{}
	childWaitDone []func(timeout time.Duration) bool
	wg            sync.WaitGroup
	mu            sync.Mutex
	once          sync.Once
}

const (
	parentStatusUnset int32 = iota
	parentStatusSet
)

var (
	parentStatus int32
)

func NewParentQuitter(quitTimeout time.Duration) *Quitter {
	parent := &Quitter{
		quitChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}

	if !atomic.CompareAndSwapInt32(&parentStatus, parentStatusUnset, parentStatusSet) {
		panic("quitter: set multiple parents")
	}

	signalChan := make(chan os.Signal)
	signal.Notify(signalChan, syscall.SIGTERM)
	signal.Notify(signalChan, syscall.SIGINT)

	go func() {
		<-signalChan
		parent.SendQuit()

		if !parent.WaitDone(quitTimeout) {
			log.Fatal("quitter: wait done timeout")
		}

		os.Exit(0)
	}()

	return parent
}

func NewChildQuitter(parent *Quitter) *Quitter {
	child := &Quitter{
		quitChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
	parent.childWaitDone = append(parent.childWaitDone, child.WaitDone)

	parent.Add(1)
	go func() {
		defer parent.Done()
		<-parent.QuitChan()
		child.SendQuit()
	}()

	return child
}

func (q *Quitter) QuitChan() <-chan struct{} {
	return q.quitChan
}

func (q *Quitter) HasToQuit() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	select {
	case <-q.quitChan:
		return true
	default:
		return false
	}
}

func (q *Quitter) SendQuit() {
	q.mu.Lock()
	defer q.mu.Unlock()

	select {
	case <-q.quitChan:
	default:
		close(q.quitChan)
	}
}

func (q *Quitter) Add(delta int) bool {
	if q.HasToQuit() {
		return false
	}

	q.wg.Add(delta)
	return true
}

func (q *Quitter) Done() {
	q.wg.Done()
}

func (q *Quitter) WaitDone(timeout time.Duration) bool {
	q.once.Do(func() {
		go func() {
			// Wait until all childs are done
			for _, f := range q.childWaitDone {
				f(timeout)
			}
			q.wg.Wait()
			close(q.doneChan)
		}()
	})

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-q.doneChan:
		return false
	case <-timer.C:
		return true
	}
}

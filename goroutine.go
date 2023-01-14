package quitter

import (
	"context"
	"reflect"
	"runtime"
)

type GoRoutineQuitter interface {
	HasToQuit() bool
	Context() context.Context
	QuitChan() <-chan struct{}
	AddGoRoutine(r GoRoutine) bool
}

type GoRoutine func(q GoRoutineQuitter)

type routineState int

const (
	// State of a goroutine that returned.
	routineStateDone routineState = iota
	// State of a goroutine that failed to return.
	routineStateNotDone
)

// NewChildQuitter returns a quitter that listens to the parent for quit events.
// If the parent has already quit, a nil quitter is returned instead.
func NewChildQuitter(parent GoRoutineQuitter, name string) *Quitter {
	parentQuitter, ok := parent.(*Quitter)
	if !ok {
		panic("quitter: parent not quitter type")
	}

	child := &Quitter{
		name:     name,
		parent:   parentQuitter,
		grsState: make(map[string]routineState),
		quitChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
	parentQuitter.childs = append(parentQuitter.childs, child)

	if !parentQuitter.add(1) {
		return nil
	}

	go func() {
		defer parentQuitter.done()
		<-parentQuitter.QuitChan()
		child.SendQuit()
	}()

	return child
}

// AddGoRoutine forks the given goroutine only if the quitter hasn't quitted yet.
func (q *Quitter) AddGoRoutine(r GoRoutine) bool {
	if !q.add(1) {
		return false
	}
	routineName := runtime.FuncForPC(reflect.ValueOf(r).Pointer()).Name()
	q.setGoRoutineState(routineName, routineStateNotDone)

	go func() {
		defer func() {
			q.setGoRoutineState(routineName, routineStateDone)
			q.done()
		}()
		r(q)
	}()

	return true
}

func (q *Quitter) setGoRoutineState(key string, value routineState) {
	q.grsStateMu.Lock()
	defer q.grsStateMu.Unlock()
	q.grsState[key] = value
}

func (q *Quitter) getTimeoutGoRoutines() []string {
	q.grsStateMu.RLock()
	defer q.grsStateMu.RUnlock()

	routines := make([]string, 0, len(q.grsState))
	for name, state := range q.grsState {
		if state == routineStateNotDone {
			routines = append(routines, name)
		}
	}

	return routines
}

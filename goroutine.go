package quitter

import (
	"reflect"
	"runtime"
)

type GoRoutine func(q *Quitter)

type routineState int

const (
	// State of a goroutine that returned.
	routineStateDone routineState = iota
	// State of a goroutine that failed to return.
	routineStateNotDone
)

// AddGoRoutine forks the given goroutine only if the quitter hasn't quitted yet.
func (q *Quitter) AddGoRoutine(r GoRoutine) bool {
	if !q.Add(1) {
		return false
	}
	routineName := runtime.FuncForPC(reflect.ValueOf(r).Pointer()).Name()
	q.setGoRoutineState(routineName, routineStateNotDone)

	go func() {
		defer func() {
			q.setGoRoutineState(routineName, routineStateDone)
			q.Done()
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

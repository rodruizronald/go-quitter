package quitter

import (
	"context"
	"errors"
	"time"
)

// quitterContext implements Context.
type quitterContext struct {
	*Quitter
}

var ErrQuitContext = errors.New("quitter: quit context")

func (*quitterContext) Deadline() (deadline time.Time, ok bool) {
	return
}

func (qc *quitterContext) Done() <-chan struct{} {
	return qc.QuitChan()
}

func (*quitterContext) Err() error {
	return ErrQuitContext
}

func (*quitterContext) Value(key any) any {
	return nil
}

func (*quitterContext) String() string {
	return "quitter.Context"
}

// Context returns the quitter context.
func (q *Quitter) Context() context.Context {
	return &quitterContext{q}
}

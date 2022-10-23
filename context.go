package quitter

import (
	"context"
	"errors"
	"time"
)

type quitterContext struct {
	*Quitter
}

var ErrContextQuitted = errors.New("quitter: quit context")

func (*quitterContext) Deadline() (deadline time.Time, ok bool) {
	return
}

func (qc *quitterContext) Done() <-chan struct{} {
	return qc.QuitChan()
}

func (*quitterContext) Err() error {
	return ErrContextQuitted
}

func (*quitterContext) Value(key any) any {
	return nil
}

func (*quitterContext) String() string {
	return "quitter.Context"
}

func (q *Quitter) Context() context.Context {
	return &quitterContext{q}
}

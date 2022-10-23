package quitter

type GoRoutine func(*Quitter)

func (q *Quitter) AddRoutine(r GoRoutine) bool {
	if !q.Add(1) {
		return false
	}
	go func() {
		defer q.Done()
		r(q)
	}()

	return true
}

package middleware

type Limiter struct {
	sem chan struct{}
}

func NewLimiter(maxConcurrent int) *Limiter {
	return &Limiter{
		sem: make(chan struct{}, maxConcurrent),
	}
}

func (l *Limiter) Acquire() bool {
	select {
	case l.sem <- struct{}{}:
		return true
	default:
		return false // overloaded
	}
}

func (l *Limiter) Release() {
	<-l.sem
}

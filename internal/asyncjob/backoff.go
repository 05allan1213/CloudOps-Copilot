package asyncjob

import (
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

type BackoffPolicy struct {
	Initial time.Duration
	Maximum time.Duration
	Jitter  float64
	Random  func() float64
}

func DefaultRetryBackoffPolicy() BackoffPolicy {
	return BackoffPolicy{Initial: time.Second, Maximum: time.Minute, Jitter: 0.2}
}

func (p BackoffPolicy) isZero() bool {
	return p.Initial == 0 && p.Maximum == 0 && p.Jitter == 0 && p.Random == nil
}

func (p BackoffPolicy) Validate() error {
	if p.Initial <= 0 || p.Maximum < p.Initial {
		return errors.New("backoff initial and maximum durations are invalid")
	}
	if p.Jitter < 0 || p.Jitter >= 1 {
		return errors.New("backoff jitter must be at least zero and less than one")
	}
	return nil
}

func (p BackoffPolicy) Delay(attempt uint32) time.Duration {
	if attempt == 0 || p.Validate() != nil {
		return 0
	}
	exponent := attempt - 1
	base := float64(p.Initial)
	if exponent < 63 {
		base *= math.Pow(2, float64(exponent))
	} else {
		base = float64(p.Maximum)
	}
	if base > float64(p.Maximum) {
		base = float64(p.Maximum)
	}
	if p.Jitter == 0 {
		return time.Duration(base)
	}
	random := p.Random
	if random == nil {
		random = rand.Float64
	}
	factor := 1 - p.Jitter + (2 * p.Jitter * random())
	delay := time.Duration(base * factor)
	if delay < 0 {
		return 0
	}
	if delay > p.Maximum {
		return p.Maximum
	}
	return delay
}

func (p BackoffPolicy) Apply(attempt uint32, requested time.Duration) time.Duration {
	delay := p.Delay(attempt)
	if requested > delay {
		return requested
	}
	return delay
}

package dispatcher

import (
	"math/rand/v2"
	"time"
)

// Backoff returns the delay before retrying after the given number of failed
// attempts (attempt >= 1): base * 2^(attempt-1), capped at max, with
// "equal jitter" (uniform in [d/2, d]) to avoid thundering herds.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := base
	for i := 1; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

package shutdown

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type Phase struct {
	Name    string
	Timeout time.Duration
	Fn      func(ctx context.Context) error
}

func Graceful(totalTimeout time.Duration, phases []Phase) {
	if len(phases) == 0 {
		return
	}

	perPhase := totalTimeout
	if totalTimeout > 0 && len(phases) > 1 {
		perPhase = totalTimeout / time.Duration(len(phases))
		if perPhase <= 0 {
			perPhase = totalTimeout
		}
	}

	for _, phase := range phases {
		timeout := perPhase
		if phase.Timeout > 0 {
			timeout = phase.Timeout
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err := phase.Fn(ctx)
		cancel()

		if err != nil {
			zap.L().Error("shutdown phase failed",
				zap.String("phase", phase.Name),
				zap.Duration("timeout", timeout),
				zap.Error(err),
			)
		} else {
			zap.L().Info("shutdown phase completed",
				zap.String("phase", phase.Name),
			)
		}
	}
}

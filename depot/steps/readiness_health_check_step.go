package steps

import (
	"fmt"
	"os"

	"code.cloudfoundry.org/executor/depot/log_streamer"
	"github.com/tedsuo/ifrit"
)

const (
// startupFailureMessage   = "Failed after %s: startup health check never passed.\n"
// timeoutCrashReason      = "Instance never healthy after %s: %s"
// healthcheckNowUnhealthy = "Instance became unhealthy: %s"
)

type readinessHealthCheckStep struct {
	// startupCheck  ifrit.Runner
	// livenessCheck ifrit.Runner
	// logger              lager.Logger
	// clock               clock.Clock
	logStreamer log_streamer.LogStreamer
	// healthCheckStreamer log_streamer.LogStreamer
	// startTimeout time.Duration
}

func NewReadinessHealthCheckStep(
	logStreamer log_streamer.LogStreamer,
) ifrit.Runner {
	return &readinessHealthCheckStep{
		logStreamer: logStreamer,
	}
}

func (step *readinessHealthCheckStep) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	fmt.Fprint(step.logStreamer.Stdout(), "Starting readiness health monitoring of container\n")
	return nil
}

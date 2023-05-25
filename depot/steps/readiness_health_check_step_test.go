package steps_test

import (
	"code.cloudfoundry.org/executor/depot/log_streamer/fake_log_streamer"
	"code.cloudfoundry.org/executor/depot/steps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	fake_runner "github.com/tedsuo/ifrit/fake_runner_v2"
)

var _ = Describe("NewHealthCheckStep", func() {
	var (
		fakeStreamer   *fake_log_streamer.FakeLogStreamer
		readinessCheck *fake_runner.TestRunner

		step ifrit.Runner
	)
	BeforeEach(func() {
		fakeStreamer = newFakeStreamer()
		readinessCheck = fake_runner.NewTestRunner()
	})

	JustBeforeEach(func() {
		step = steps.NewReadinessHealthCheckStep(readinessCheck, fakeStreamer)
		_ = ifrit.Background(step)
	})

	AfterEach(func() {
		// Eventually(readinessCheck.RunCallCount).Should(Equal(1))
		readinessCheck.EnsureExit()
	})

	Describe("Run", func() {
		It("emits a message to the applications log stream", func() {
			Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
				gbytes.Say("Starting readiness health monitoring of container\n"),
			)
		})

		FIt("Runs the readiness check", func() {
			Eventually(readinessCheck.RunCallCount).Should(Equal(1))
		})

		Context("when optional check definition properties are missing", func() {
			It("uses sane defaults", func() {})
		})

		Context("when the readiness check is first run", func() { // the --until-it-fails bit
			Context("when the readiness check eventually succeeds", func() {
				It("publishes something to some channel", func() {})
			})
			Context("when the readiness check continuously fails", func() {
				It("eventually timesout", func() {})
				It("logs with nice message", func() {})
				It("does not complete with a failure. It keeps running??? Maybe??? Even on the first run>???? Is there a timeout???", func() {})
			})
		})

		Context("when there are multiple readiness checks", func() {}) // is this possible???
	})

	Describe("Signalling", func() {}) // Think about later.

	//TODO eventually think about how this should play with startup.
})

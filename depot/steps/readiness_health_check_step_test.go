package steps_test

import (
	"os"

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
		fakeStreamer    *fake_log_streamer.FakeLogStreamer
		untilReadyCheck *fake_runner.TestRunner
		process         ifrit.Process

		step ifrit.Runner
	)
	BeforeEach(func() {
		fakeStreamer = newFakeStreamer()
		untilReadyCheck = fake_runner.NewTestRunner()
	})

	JustBeforeEach(func() {
		step = steps.NewReadinessHealthCheckStep(untilReadyCheck, fakeStreamer)
		process = ifrit.Background(step)
	})

	AfterEach(func() {
		// Eventually(untilReadyCheck.RunCallCount).Should(Equal(1))
		untilReadyCheck.EnsureExit()
	})

	Describe("Run", func() {
		It("emits a message to the applications log stream", func() {
			Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
				gbytes.Say("Starting readiness health monitoring of container\n"),
			)
		})

		It("Runs the untilReady check", func() {
			Eventually(untilReadyCheck.RunCallCount).Should(Equal(1))
		})

		Context("the untilReady check succeeds", func() {
			It("emits a message to the applications log stream", func() {
				Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).ShouldNot(
					gbytes.Say("App is ready!\n"),
				)

				untilReadyCheck.TriggerExit(nil)

				Eventually(fakeStreamer.Stdout().(*gbytes.Buffer)).Should(
					gbytes.Say("App is ready!\n"),
				)
			})

			It("becomes ready", func() {
				untilReadyCheck.TriggerExit(nil)
				Eventually(process.Ready()).Should(BeClosed())
			})
		})

		Context("the untilReady check fails", func() {
			It("....idk what to do in this case", func() {})
		})

		Context("when optional check definition properties are missing", func() {
			It("uses sane defaults", func() {})
		})

		Context("when the readiness check eventually succeeds", func() {
			It("??? can we even test for this???", func() {})
		})
		Context("when the readiness check continuously fails", func() {
			It("eventually timesout", func() {})
			It("logs with nice message", func() {})
			It("does not complete with a failure. It keeps running??? Maybe??? Even on the first run>???? Is there a timeout???", func() {})
		})

		Context("when there are multiple readiness checks", func() {}) // is this possible???
	})

	Describe("Signalling", func() {
		Context("while doing untilReady check", func() {
			It("cancels the in-flight check", func() {
				Eventually(untilReadyCheck.RunCallCount).Should(Equal(1))

				process.Signal(os.Interrupt)
				Eventually(untilReadyCheck.WaitForCall()).Should(Receive(Equal(os.Interrupt)))
				untilReadyCheck.TriggerExit(nil)
				Eventually(process.Wait()).Should(Receive(Equal(new(steps.CancelledError))))
			})
		})

	}) // Think about later.

	//TODO eventually think about how this should play with startup.
})

package action_runner_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestActionRunner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ActionRunner Suite")
}
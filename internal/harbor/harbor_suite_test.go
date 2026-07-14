package harbor

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHarbor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Harbor Suite")
}

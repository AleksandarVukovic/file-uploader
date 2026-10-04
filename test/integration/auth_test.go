//go:build e2e

package integration

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Login", func() {
	It("issues a token for the hardcoded credential", func() {
		resp, err := login(e2eUsername, e2ePassword)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("rejects an invalid credential", func() {
		resp, err := login(e2eUsername, "wrong-password")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("rejects a payload failing the design's validation rules", func() {
		resp, err := login(e2eUsername, "short")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})
})

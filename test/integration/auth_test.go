//go:build e2e

package integration

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Login", func() {
	It("issues a token for the registered user", func() {
		resp, err := login(e2eUsername, e2ePassword)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("rejects a wrong password", func() {
		resp, err := login(e2eUsername, "Wrong-Password-1")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("rejects an unknown user the same way", func() {
		resp, err := login("nobody_here", e2ePassword)
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

var _ = Describe("Register", func() {
	It("creates a user who can then log in", func() {
		resp, err := register("registered_user", "registered.user@example.com", "Registered", "User", "Registered-Pass-1")
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var body struct {
			ID        int64  `json:"id"`
			Username  string `json:"username"`
			Email     string `json:"email"`
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		}
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body.ID).To(BeNumerically(">", 0))
		Expect(body.Username).To(Equal("registered_user"))
		Expect(body.Email).To(Equal("registered.user@example.com"))
		Expect(body.FirstName).To(Equal("Registered"))
		Expect(body.LastName).To(Equal("User"))

		loginResp, err := login("registered_user", "Registered-Pass-1")
		Expect(err).NotTo(HaveOccurred())
		defer loginResp.Body.Close()
		Expect(loginResp.StatusCode).To(Equal(http.StatusOK))
	})

	DescribeTable("rejects a taken username or email",
		func(username, email string) {
			resp, err := register(username, email, e2eFirstName, e2eLastName, e2ePassword)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		},
		Entry("same username", e2eUsername, "another.address@example.com"),
		Entry("same email in a different case", "another_user", "E2E.User@Example.com"),
	)

	DescribeTable("rejects data user-service refuses",
		func(username, password string) {
			resp, err := register(username, "refused@example.com", e2eFirstName, e2eLastName, password)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		},
		Entry("reserved username", "admin", e2ePassword),
		Entry("password missing a special character", "weak_password_user", "NoSpecialChar1"),
	)

	DescribeTable("rejects a payload failing the design's validation rules",
		func(username, email, firstName, lastName, password string) {
			resp, err := register(username, email, firstName, lastName, password)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		},
		Entry("uppercase username", "Upper_User", "upper@example.com", e2eFirstName, e2eLastName, e2ePassword),
		Entry("invalid email", "bad_email_user", "not-an-email", e2eFirstName, e2eLastName, e2ePassword),
		Entry("short password", "short_pass_user", "short@example.com", e2eFirstName, e2eLastName, "Ab1!"),
		Entry("empty first name", "empty_first_user", "empty.first@example.com", "", e2eLastName, e2ePassword),
		Entry("empty last name", "empty_last_user", "empty.last@example.com", e2eFirstName, "", e2ePassword),
		Entry("first name longer than 100 characters", "long_first_user", "long.first@example.com", strings.Repeat("a", 101), e2eLastName, e2ePassword),
		Entry("last name longer than 100 characters", "long_last_user", "long.last@example.com", e2eFirstName, strings.Repeat("a", 101), e2ePassword),
	)
})

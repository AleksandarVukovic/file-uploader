package design

import . "goa.design/goa/v3/dsl"

var _ = Service("auth", func() {
	Description("Service provides authentication functionality.")

	Method("login", func() {
		Description("Authenticates a user and returns a JWT token.")

		Payload(func() {
			Attribute("username", String, "Username of the user", func() {
				MinLength(3)
				MaxLength(50)
				Pattern("^[a-zA-Z0-9._-]+$")
				Example("john_doe")
			})
			Attribute("password", String, "Password of the user", func() {
				MinLength(8)
				MaxLength(64)
				Example("s3cr3t#s")
			})
			Required("username", "password")
		})

		Result(func() {
			Attribute("token", String, "JWT token for authenticated user", func() {
				Example("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c")
			})
			Required("token")
		})

		Error("invalid_credentials", ErrorResult, "Invalid username or password.")
		Error("unavailable", ErrorResult, "Service unavailable.")
		Error("internal_error", ErrorResult, "Fault in the service.")

		HTTP(func() {
			POST("/login")
			Response(StatusOK)
			Response("invalid_credentials", StatusUnauthorized)
			Response("unavailable", StatusServiceUnavailable)
			Response("internal_error", StatusInternalServerError)
		})
	})
})

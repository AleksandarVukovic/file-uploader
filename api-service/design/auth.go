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

	Method("register", func() {
		Description("Creates a new user account.")

		Payload(func() {
			Attribute("username", String, "Username of the new user", func() {
				MinLength(3)
				MaxLength(50)
				Pattern(`^[a-z][a-z0-9_]*$`)
				Example("john_doe")
			})
			Attribute("email", String, "Email address of the new user", func() {
				Format(FormatEmail)
				Example("john.doe@example.com")
			})
			Attribute("password", String, "Password of the new user", func() {
				MinLength(8)
				MaxLength(72)
				Pattern(`^[\x21-\x7E]+$`)
				Example("Correct-Horse-42")
			})
			Required("username", "email", "password")
		})

		Result(func() {
			Attribute("id", Int64, "Unique identifier of the new user", func() {
				Example(123)
			})
			Attribute("username", String, "Username of the new user", func() {
				Example("john_doe")
			})
			Attribute("email", String, "Email address of the new user", func() {
				Example("john.doe@example.com")
			})
			Required("id", "username", "email")
		})

		Error("invalid_input", ErrorResult, "The user data was rejected.")
		Error("user_exists", ErrorResult, "Username or email is already taken.")
		Error("unavailable", ErrorResult, "Service unavailable.")
		Error("internal_error", ErrorResult, "Fault in the service.")

		HTTP(func() {
			POST("/register")
			Response(StatusCreated)
			Response("invalid_input", StatusBadRequest)
			Response("user_exists", StatusConflict)
			Response("unavailable", StatusServiceUnavailable)
			Response("internal_error", StatusInternalServerError)
		})
	})
})

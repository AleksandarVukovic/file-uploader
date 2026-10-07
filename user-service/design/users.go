package design

import . "goa.design/goa/v3/dsl"

var _ = Service("users", func() {
	Description("Service manages user-related operations.")

	Error("bad_request", ErrorResult, "Invalid request payload.")
	Error("not_found", ErrorResult, "User not found.")
	Error("internal_error", ErrorResult, "Fault in the service.")

	Method("getByUsername", func() {
		Description("Retrieves a user by their username.")

		Payload(func() {
			Field(1, "username", String, "The username of the user to retrieve", func() {
				MinLength(3)
				MaxLength(50)
				Pattern(`^[a-z][a-z0-9_]*$`)
				Example("john_doe")
			})
			Required("username")
		})

		Result(User)

		GRPC(func() {
			Response(CodeOK)
			Response("bad_request", CodeInvalidArgument)
			Response("not_found", CodeNotFound)
			Response("internal_error", CodeInternal)
		})
	})

	Method("create", func() {
		Description("Creates a new user.")

		Payload(func() {
			Field(1, "username", String, "The username of the new user", func() {
				MinLength(3)
				MaxLength(50)
				Pattern(`^[a-z][a-z0-9_]*$`)
				Example("john_doe")
			})
			Field(2, "email", String, "The email address of the new user", func() {
				Format(FormatEmail)
				Example("john.doe@example.com")
			})
			Field(3, "password", String, "The plaintext password; hashed by the service before storage", func() {
				MinLength(8)
				MaxLength(72) // because that's max limit of bcrypt input
				Pattern(`^[\x21-\x7E]+$`)
				Example("CorrectHorse-42")
			})
			Required("username", "email", "password")
		})

		Result(User)

		Error("already_exists", ErrorResult, "Username or email is already taken.")

		GRPC(func() {
			Response(CodeOK)
			Response("bad_request", CodeInvalidArgument)
			Response("already_exists", CodeAlreadyExists)
			Response("internal_error", CodeInternal)
		})
	})

	Method("authenticate", func() {
		Description("Verifies a username/password pair and returns the matching user.")

		Payload(func() {
			Field(1, "username", String, "The username to authenticate", func() {
				MinLength(3)
				MaxLength(50)
				Pattern(`^[a-z][a-z0-9_]*$`)
				Example("john_doe")
			})
			Field(2, "password", String, "The plaintext password to verify", func() {
				MinLength(8)
				MaxLength(72)
				Example("CorrectHorse-42")
			})
			Required("username", "password")
		})

		Result(User)

		Error("unauthorized", ErrorResult, "Invalid username or password.")

		GRPC(func() {
			Response(CodeOK)
			Response("bad_request", CodeInvalidArgument)
			Response("unauthorized", CodeUnauthenticated)
			Response("internal_error", CodeInternal)
		})
	})
})

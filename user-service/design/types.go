package design

import . "goa.design/goa/v3/dsl"

var User = ResultType("application/vnd.user", "User", func() {
	Description("A user of the system.")

	Attributes(func() {
		Field(1, "id", Int64, "Unique identifier of the user", func() {
			Example(123)
		})
		Field(2, "username", String, "The username of the user", func() {
			Example("john_doe")
		})
		Field(3, "email", String, "The email address of the user", func() {
			Format(FormatEmail)
			Example("john.doe@example.com")
		})
		Field(4, "createdAt", String, "The timestamp when the user was created", func() {
			Format(FormatDateTime)
			Example("2023-01-01T12:00:00Z")
		})
		Field(5, "updatedAt", String, "The timestamp when the user was last updated", func() {
			Format(FormatDateTime)
			Example("2023-01-02T12:00:00Z")
		})
		Required("id", "username", "email", "createdAt", "updatedAt")
	})
})

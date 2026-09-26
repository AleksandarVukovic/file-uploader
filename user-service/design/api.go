package design

import . "goa.design/goa/v3/dsl"

var _ = API("user-service", func() {
	Title("User Service")
	Description("Service for handling user-related operations")
	Server("user-service", func() {
		Host("localhost", func() {
			URI("http://localhost:8080")
		})
	})
})

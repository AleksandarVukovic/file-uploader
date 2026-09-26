package design

import . "goa.design/goa/v3/dsl"

var _ = API("api-service", func() {
	Title("API Service")
	Description("Service for handling public API requests")
	Server("api-service", func() {
		Host("localhost", func() {
			URI("http://localhost:8080")
		})
	})
})

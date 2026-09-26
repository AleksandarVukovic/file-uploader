package design

import . "goa.design/goa/v3/dsl"

var _ = API("file-service", func() {
	Title("File Service")
	Description("Service for handling file operations")
	Server("file-service", func() {
		Host("localhost", func() {
			URI("http://localhost:8080")
		})
	})
})

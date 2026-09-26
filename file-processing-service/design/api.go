package design

import . "goa.design/goa/v3/dsl"

var _ = API("file-processing-service", func() {
	Title("File Processing Service")
	Description("Service for processing internal files")
	Server("file-processing-service", func() {
		Host("localhost", func() {
			URI("http://localhost:8080")
		})
	})
})

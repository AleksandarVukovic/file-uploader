package design

import . "goa.design/goa/v3/dsl"

var _ = Service("health", func() {
	Description("Service provides health check functionality.")

	Method("health", func() {
		Description("Returns the health status of the service.")

		Result(func() {
			Attribute("status", String, "Health status of the service", func() {
				Example("ok")
			})
			Required("status")
		})

		Error("unavailable", ErrorResult, "Service unavailable.")
		Error("internal_error", ErrorResult, "Fault in the service.")

		HTTP(func() {
			GET("/health")
			Response(StatusOK)
			Response("unavailable", StatusServiceUnavailable)
			Response("internal_error", StatusInternalServerError)
		})
	})
})

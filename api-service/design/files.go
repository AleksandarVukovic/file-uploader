package design

import . "goa.design/goa/v3/dsl"

var _ = Service("files", func() {
	Description("Service manages upload of single files.")
	HTTP(func() {
		Path("/api/v1")
	})
	Error("bad_request", ErrorResult, "Invalid request payload.")
	Error("internal_error", ErrorResult, "Fault in the service.")

	Method("upload", func() {
		Description("Uploads a whole file with max size of 10Mb.")

		Payload(func() {
			Attribute("filename", String, "Name of the file being uploaded", func() {
				Pattern(`^[A-Za-z0-9._-]+\.csv$`)
				Example("users.csv")
			})
			Attribute("size", Int64, "Total size of the file in bytes (max 10Mb)", func() {
				Minimum(100)
				Maximum(10485760) // 10Mb
				Example(1048576)
			})
			Attribute("contentType", String, "MIME type of the file", func() {
				Enum("text/csv")
				Default("text/csv")
				Example("text/csv")
			})
			Attribute("checksum", String, "Expected SHA-256 checksum of the file", func() {
				Pattern("^[a-f0-9]{64}$")
				Example("9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
			})
			Required("filename", "size", "contentType", "checksum")
		})

		HTTP(func() {
			POST("/files/upload")
			SkipRequestBodyEncodeDecode()
			Header("filename:Content-Disposition")
			Header("size:Content-Length")
			Header("contentType:Content-Type")
			Header("checksum:X-Checksum")
			Response(StatusNoContent)
			Response("bad_request", StatusBadRequest)
			Response("internal_error", StatusInternalServerError)
		})
	})

})

package events

const (
	AggregateTypeFile     = "file"
	EventTypeFileUploaded = "FileUploaded"
)

type FileUploadedPayload struct {
	FileID      string `json:"fileId"`
	UserID      int64  `json:"userId"`
	RootDir     string `json:"rootDir"`
	Path        string `json:"path"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Checksum    string `json:"checksum"` // hex-encoded SHA-256
}

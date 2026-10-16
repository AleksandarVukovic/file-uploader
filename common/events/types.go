package events

const (
	EventTypeUsersImported  = "UsersImported"
	RoutingKeyUsersImported = "user.imported"
)

type UserPayload struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

type UsersImportedPayload struct {
	FileID string        `json:"fileId"`
	Batch  int           `json:"batch"`
	Users  []UserPayload `json:"users"`
}

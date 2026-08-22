package respond

type GetUserListRespond struct {
	Uuid      string `json:"uuid"`
	Nickname  string `json:"nickname"`
	Telephone string `json:"telephone"`
	Email     string `json:"email"`
	Avatar    string `json:"avatar"`
	Status    int8   `json:"status"`
	IsAdmin   int8   `json:"is_admin"`
	IsDeleted bool   `json:"is_deleted"`
}

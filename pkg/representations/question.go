package representations

type Question struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Answer string `json:"-"` // Exclude answer from default JSON responses
	Type   string `json:"type"`
}

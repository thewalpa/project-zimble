package representations

import "time"

type User struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"` // Or Email, depending on your login method
	Email          string    `json:"email"`
	HashedPassword string    `json:"-"` // Never expose hash
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	// Maybe verification status, etc.
}

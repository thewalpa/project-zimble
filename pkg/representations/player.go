package representations

import "time"

type Player struct {
	ID                string    `json:"id"`       // Player's unique ID (could be same as UserID or separate UUID)
	UserID            string    `json:"userId"`   // Foreign key linking to the User account
	Nickname          string    `json:"nickname"` // In-game name, potentially changeable by the user
	MatchmakingRating int       `json:"mmr"`      // Example stat for matchmaking
	GamesPlayed       int       `json:"gamesPlayed"`
	Wins              int       `json:"wins"`
	Losses            int       `json:"losses"`
	TotalScore        int64     `json:"totalScore"` // Accumulated score across games
	LastSeenAt        time.Time `json:"lastSeenAt"` // Useful for analytics/cleanup
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	// Add any other stats you want to track for analytics
	// e.g., AverageAnswerTime, FavoriteCategory, etc.
}

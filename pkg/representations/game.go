package representations

import "sync"

type GameStatus string

const (
	Waiting    GameStatus = "waiting"
	InProgress GameStatus = "inprogress"
	Finished   GameStatus = "finished"
)

type Game struct {
	ID                 string             `json:"id"`
	Players            map[string]*Player `json:"players"` // Map PlayerID to Player struct
	Questions          []Question         `json:"-"`       // Keep questions internal for now
	CurrentQuestionIdx int                `json:"currentQuestionIndex"`
	Status             GameStatus         `json:"status"`
	// To handle answers in a duel, you might need something like:
	// CurrentAnswers map[string]string // Map PlayerID to their submitted answer for the current question
	// ReadyForNext   map[string]bool    // Map PlayerID to whether they are ready for the next question
	Mu sync.RWMutex // To handle concurrent access safely
}

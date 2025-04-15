package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	// Import your models package if you created one
	// "github.com/yourusername/quizapp/models"
)

// --- Use the models defined above (or import them) ---
// (Paste or import the struct definitions and global variables here for now)
type Question struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Answer string `json:"-"`
}
type Player struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Score int    `json:"score"`
}
type GameStatus string

const (
	Waiting    GameStatus = "waiting"
	InProgress GameStatus = "inprogress"
	Finished   GameStatus = "finished"
)

type Game struct {
	ID                string             `json:"id"`
	Players           map[string]*Player `json:"players"`
	Questions         []Question         `json:"-"`
	CurrentQuestionIx int                `json:"currentQuestionIndex"`
	Status            GameStatus         `json:"status"`
	mu                sync.RWMutex
}

var games = make(map[string]*Game)
var gameMutex = sync.RWMutex{}
var questionBank = []Question{
	{ID: "q1", Text: "What is the capital of France?", Answer: "Paris"},
	{ID: "q2", Text: "What is 2 + 2?", Answer: "4"},
	{ID: "q3", Text: "What language is this backend written in?", Answer: "Go"},
}

// --- End Models/Globals ---

// --- Helper Functions ---
func generateID() string {
	// Simple ID generation (not guaranteed unique in high concurrency)
	return fmt.Sprintf("%d", time.Now().UnixNano()+int64(rand.Intn(1000)))
}

// --- API Handlers ---

// POST /games - Create a new duel game
func createGameHandler(c *gin.Context) {
	// In a real app, you'd get player names/IDs from the request body
	player1Name := "Player1"
	player2Name := "Player2"

	gameID := generateID()
	player1ID := generateID()
	player2ID := generateID()

	newGame := &Game{
		ID: gameID,
		Players: map[string]*Player{
			player1ID: {ID: player1ID, Name: player1Name, Score: 0},
			player2ID: {ID: player2ID, Name: player2Name, Score: 0},
		},
		Questions:         append([]Question{}, questionBank...), // Copy questions
		CurrentQuestionIx: 0,
		Status:            InProgress, // Start immediately for simplicity
		mu:                sync.RWMutex{},
	}

	gameMutex.Lock()
	games[gameID] = newGame
	gameMutex.Unlock()

	fmt.Printf("Created Game: %s with players %s, %s\n", gameID, player1ID, player2ID)
	// Return the game state, including player IDs so the client knows them
	c.JSON(http.StatusCreated, newGame)
}

// GET /games/:gameId - Get current game state
func getGameHandler(c *gin.Context) {
	gameID := c.Param("gameId")

	gameMutex.RLock()
	game, exists := games[gameID]
	gameMutex.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}

	game.mu.RLock()
	defer game.mu.RUnlock()

	// Clone the game data to avoid race conditions if sending complex state
	// For now, sending the locked data is okay for this simple example
	c.JSON(http.StatusOK, game)
}

// GET /games/:gameId/question - Get the current question for the game
func getQuestionHandler(c *gin.Context) {
	gameID := c.Param("gameId")

	gameMutex.RLock()
	game, exists := games[gameID]
	gameMutex.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}

	game.mu.RLock()
	defer game.mu.RUnlock()

	if game.Status != InProgress {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Game is not in progress"})
		return
	}

	if game.CurrentQuestionIx >= len(game.Questions) {
		c.JSON(http.StatusOK, gin.H{"message": "No more questions"})
		// Optionally set game status to Finished here
		// game.Status = Finished
		return
	}

	currentQuestion := game.Questions[game.CurrentQuestionIx]
	// Send only the public info (ID and Text)
	c.JSON(http.StatusOK, gin.H{
		"id":    currentQuestion.ID,
		"text":  currentQuestion.Text,
		"index": game.CurrentQuestionIx,
	})
}

// POST /games/:gameId/answer - Submit an answer
type AnswerPayload struct {
	PlayerID string `json:"playerId"`
	Answer   string `json:"answer"`
}

func submitAnswerHandler(c *gin.Context) {
	gameID := c.Param("gameId")

	var payload AnswerPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	gameMutex.RLock()
	game, exists := games[gameID]
	gameMutex.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Game not found"})
		return
	}

	game.mu.Lock() // Need write lock to potentially update score/state
	defer game.mu.Unlock()

	if game.Status != InProgress {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Game is not in progress"})
		return
	}

	player, playerExists := game.Players[payload.PlayerID]
	if !playerExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Player not found in this game"})
		return
	}

	if game.CurrentQuestionIx >= len(game.Questions) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Game has already finished"})
		return
	}

	correctAnswer := game.Questions[game.CurrentQuestionIx].Answer
	isCorrect := (payload.Answer == correctAnswer) // Case-sensitive for now

	result := "Incorrect"
	if isCorrect {
		player.Score++ // Add points
		result = "Correct"
		fmt.Printf("Game %s: Player %s answered correctly!\n", gameID, payload.PlayerID)
	} else {
		fmt.Printf("Game %s: Player %s answered incorrectly.\n", gameID, payload.PlayerID)
	}

	// Simple Duel Logic: Advance question immediately after an answer (could be improved)
	// In a real duel, you'd wait for both players or a timer.
	game.CurrentQuestionIx++
	if game.CurrentQuestionIx >= len(game.Questions) {
		game.Status = Finished
		fmt.Printf("Game %s finished.\n", gameID)
	}

	c.JSON(http.StatusOK, gin.H{
		"result":        result,
		"yourScore":     player.Score,
		"correctAnswer": correctAnswer, // Reveal answer after submission
		"gameStatus":    game.Status,
	})
}

// --- Main Function ---
func main() {
	rand.Seed(time.Now().UnixNano()) // Seed random number generator

	router := gin.Default()

	// API Routes
	api := router.Group("/api")
	{
		api.POST("/games", createGameHandler)
		api.GET("/games/:gameId", getGameHandler)
		api.GET("/games/:gameId/question", getQuestionHandler)
		api.POST("/games/:gameId/answer", submitAnswerHandler)
	}

	// Serve the simple web view (Phase 2)
	// This tells Gin to serve static files from the 'web' directory
	router.Static("/web", "./web")
	// Redirect root to the web view
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/web")
	})

	fmt.Println("Server starting on http://localhost:8080")
	if err := router.Run(":8080"); err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}

package representations

// QuestionType defines the different kinds of questions
type QuestionType string

const (
	TypeMultipleChoiceSingle   QuestionType = "multiple_choice_single"   // 1 of N
	TypeMultipleChoiceMultiple QuestionType = "multiple_choice_multiple" // M of N
	TypeTrueFalse              QuestionType = "true_false"
	TypeFillInBlank            QuestionType = "fill_in_blank"
	TypeOrdering               QuestionType = "ordering"
	TypeEstimation             QuestionType = "estimation"
	TypeOddOneOut              QuestionType = "odd_one_out"
	// Add more as needed (e.g., TypeMatching)
)

type Question struct {
	ID   string       `json:"id"`
	Type QuestionType `json:"type"`
	Text string       `json:"text"` // Main question, statement, or instruction

	// --- Data Fields (used depending on Type) ---

	// For MultipleChoice*, TrueFalse (implicitly ["True", "False"]), Ordering, OddOneOut
	Options []string `json:"options,omitempty"`

	// --- Answer Definitions (likely excluded from JSON response initially) ---

	// For MC Single Correct (index in Options), True/False (0=False, 1=True)
	CorrectAnswerIndex *int `json:"-"`

	// For MC Multiple Correct (indices in Options), OddOneOut (if multiple correct)
	CorrectAnswerIndices []int `json:"-"`

	// For FillInBlank (list of acceptable strings), Estimation (target value for comparison)
	CorrectAnswers []string `json:"-"`

	// For Ordering (indices of Options slice in correct order)
	CorrectOrderIndices []int `json:"-"`

	// For Estimation (defines the acceptable range)
	AcceptableMinValue *float64 `json:"-"` // Use pointers for optional values
	AcceptableMaxValue *float64 `json:"-"`

	// --- Optional Metadata ---
	TimeLimitSeconds int    `json:"timeLimitSeconds,omitempty"` // Time allowed for this question
	Category         string `json:"category,omitempty"`
	Difficulty       int    `json:"difficulty,omitempty"` // e.g., 1-5
}

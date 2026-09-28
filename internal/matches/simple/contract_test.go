package simple

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

func TestContract(t *testing.T) {
	enginetest.Contract(t, engine(t), enginetest.Config{Matches: 300, Knockouts: 400})
}

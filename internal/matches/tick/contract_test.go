package tick

import (
	"testing"

	"github.com/thewalpa/project-zimble/internal/matches/enginetest"
)

func TestContract(t *testing.T) {
	cfg := enginetest.Config{Matches: 60, Knockouts: 150}
	if testing.Short() {
		cfg = enginetest.Config{Matches: 10, Knockouts: 50}
	}
	enginetest.Contract(t, engine(t), cfg)
}

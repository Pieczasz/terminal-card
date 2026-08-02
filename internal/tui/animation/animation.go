package animation

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

const FPS = 60

type FrameMsg time.Time

func Tick() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(FPS), func(t time.Time) tea.Msg {
		return FrameMsg(t)
	})
}

const (
	springFrequency = 15.0
	springDamping   = 1.0 // critical: settles without overshooting
)

// DefaultSpring settles quickly and does not oscillate, so a card lifted by the
// selection cursor arrives at its target instead of wobbling around it.
func DefaultSpring() harmonica.Spring {
	return harmonica.NewSpring(harmonica.FPS(FPS), springFrequency, springDamping)
}

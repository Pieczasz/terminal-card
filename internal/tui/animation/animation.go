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

// DefaultSpring returns a bouncy spring.
func DefaultSpring() harmonica.Spring {
	// frequency: higher is faster
	// damping: 1.0 is no bounce
	spring := harmonica.NewSpring(harmonica.FPS(FPS), 15.0, 1.0)
	return spring
}

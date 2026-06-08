package router

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type MockModel struct {
	handledMsg tea.Msg
}

func (m MockModel) Init() tea.Cmd { return nil }
func (m MockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.handledMsg = msg
	return m, nil
}
func (m MockModel) View() string { return "mock" }

func TestRouter_RegistrationAndRouting(t *testing.T) {
	t.Parallel()
	r := New(GlobalContext{})

	r.Register("mock", func(gc GlobalContext, a any) tea.Model {
		return MockModel{}
	})

	cmd := r.Goto("mock", nil)
	assert.Nil(t, cmd)
	assert.NotNil(t, r.active)
	assert.Equal(t, "mock", r.activeKey)
}

func TestRouter_UpdatePropagation(t *testing.T) {
	t.Parallel()
	r := New(GlobalContext{})

	r.Register("mock", func(gc GlobalContext, a any) tea.Model {
		return MockModel{}
	})

	r.Goto("mock", nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}
	newModel, _ := r.Update(keyMsg)

	assert.Equal(t, keyMsg, newModel.(*Router).active.(MockModel).handledMsg)
}

func TestRouter_WindowSize(t *testing.T) {
	t.Parallel()
	r := New(GlobalContext{})

	sizeMsg := tea.WindowSizeMsg{Width: 100, Height: 200}
	newModel, _ := r.Update(sizeMsg)

	assert.Equal(t, 100, newModel.(*Router).Global.Width)
	assert.Equal(t, 200, newModel.(*Router).Global.Height)
}

func TestRouter_ChangeViewMsg(t *testing.T) {
	t.Parallel()
	r := New(GlobalContext{})
	r.Register("mock", func(gc GlobalContext, a any) tea.Model {
		return MockModel{}
	})

	msg := ChangeViewMsg{ViewName: "mock"}
	r.Update(msg)

	assert.Equal(t, "mock", r.activeKey)
}

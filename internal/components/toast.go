// Package components holds the reusable Bubble Tea components shared by the
// application pages.
//
// Every component is a self-contained [tea.Model] that owns its own state
// and communicates with the rest of the application through messages.
package components

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/incurl/flint/internal/theme"
)

// ToastLevel classifies a toast notification.
type ToastLevel int

const (
	// ToastInfo is a neutral notification.
	ToastInfo ToastLevel = iota
	// ToastSuccess reports a successful operation.
	ToastSuccess
	// ToastWarning reports a cautionary event.
	ToastWarning
	// ToastError reports a failure.
	ToastError
)

// String implements [fmt.Stringer].
func (l ToastLevel) String() string {
	switch l {
	case ToastSuccess:
		return "success"
	case ToastWarning:
		return "warning"
	case ToastError:
		return "error"
	default:
		return "info"
	}
}

// ToastMsg is the message emitted when a toast is displayed.
//
// The root model routes it to the toast component.
type ToastMsg struct {
	// Level classifies the notification.
	Level ToastLevel
	// Text is the notification body.
	Text string
	// Duration is how long the toast stays visible.
	//
	// Zero falls back to [DefaultToastDuration].
	Duration time.Duration
}

// DefaultToastDuration is how long a toast stays visible by default.
const DefaultToastDuration = 4 * time.Second

// Toast is the transient notification component.
//
// It renders a single line at the bottom of the screen and removes itself
// automatically once its timeout expires.
type Toast struct {
	// theme is the shared style palette.
	theme *theme.Theme

	// level classifies the current notification.
	level ToastLevel
	// text is the current notification body.
	text string
	// expiresAt is the time at which the toast is removed.
	expiresAt time.Time
	// visible reports whether a toast is currently displayed.
	visible bool
	// width is the available rendering width.
	width int
}

// NewToast creates a hidden toast component.
func NewToast(t *theme.Theme) *Toast {
	if t == nil {
		t = theme.Default()
	}

	return &Toast{theme: t}
}

// Show displays a new notification.
//
// It returns the command that removes the toast once its timeout expires.
func (t *Toast) Show(level ToastLevel, text string) tea.Cmd {
	duration := DefaultToastDuration
	t.level = level
	t.text = text
	t.visible = true
	t.expiresAt = time.Now().Add(duration)

	return tea.Tick(duration, func(time.Time) tea.Msg {
		return ToastExpiredMsg{}
	})
}

// ShowWithDuration displays a new notification with a custom duration.
//
// It returns the command that removes the toast once its timeout expires.
func (t *Toast) ShowWithDuration(level ToastLevel, text string, duration time.Duration) tea.Cmd {
	if duration <= 0 {
		duration = DefaultToastDuration
	}

	t.level = level
	t.text = text
	t.visible = true
	t.expiresAt = time.Now().Add(duration)

	return tea.Tick(duration, func(time.Time) tea.Msg {
		return ToastExpiredMsg{}
	})
}

// Hide removes the currently displayed toast.
func (t *Toast) Hide() {
	t.visible = false
	t.text = ""
}

// Visible reports whether a toast is currently displayed.
func (t *Toast) Visible() bool { return t.visible }

// Init implements [tea.Model].
func (t *Toast) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (t *Toast) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ToastExpiredMsg:
		// The expiry message is scheduled by the Show* methods, so it
		// always refers to the toast that is currently displayed.
		t.visible = false
		t.text = ""
		t.expiresAt = time.Time{}
	case tea.WindowSizeMsg:
		t.width = msg.Width
	}

	return t, nil
}

// View implements [tea.Model].
func (t *Toast) View() string {
	if !t.visible || t.text == "" {
		return ""
	}

	label := t.theme.StatusColor(t.level.String())
	icon := t.icon()

	body := label.Render(icon+" ") + t.theme.Base.Render(t.text)

	return t.theme.Toast.Render(body)
}

// icon returns the symbol prefixed to the toast body.
func (t *Toast) icon() string {
	switch t.level {
	case ToastSuccess:
		return "✓"
	case ToastWarning:
		return "!"
	case ToastError:
		return "✗"
	default:
		return "•"
	}
}

// ToastExpiredMsg is emitted when a toast reaches its timeout.
type ToastExpiredMsg struct{}

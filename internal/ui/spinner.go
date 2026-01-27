package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"golang.org/x/term"
)

// Spinner provides a simple animated spinner for long operations
type Spinner struct {
	message string
	done    chan struct{}
}

// NewSpinner creates a new spinner with the given message
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		done:    make(chan struct{}),
	}
}

// Start begins the spinner animation in the background
func (s *Spinner) Start() {
	// Accessible mode or non-TTY: just print static message
	if styles.IsAccessible() || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(s.message + "...")
		return
	}

	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		style := lipgloss.NewStyle().Foreground(styles.Accent)
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.done:
				// Clear the spinner line
				fmt.Print("\r\033[K")
				return
			case <-ticker.C:
				frame := style.Render(frames[i%len(frames)])
				fmt.Printf("\r%s %s", frame, s.message)
				i++
			}
		}
	}()
}

// Stop stops the spinner
func (s *Spinner) Stop() {
	close(s.done)
}

// Success stops the spinner and shows a success message
func (s *Spinner) Success(msg string) {
	s.Stop()
	time.Sleep(10 * time.Millisecond) // Let the goroutine clean up
	fmt.Println(styles.SuccessMsg(msg))
}

// Error stops the spinner and shows an error message
func (s *Spinner) Error(msg string) {
	s.Stop()
	time.Sleep(10 * time.Millisecond)
	fmt.Println(styles.ErrorMsg(msg))
}

// ══════════════════════════════════════════════════════════════════════════
// Progress bar for operations with known progress
// ══════════════════════════════════════════════════════════════════════════

// Progress represents a progress bar
type Progress struct {
	total   int
	current int
	label   string
	width   int
}

// NewProgress creates a new progress bar
func NewProgress(label string, total int) *Progress {
	return &Progress{
		label: label,
		total: total,
		width: 30,
	}
}

// Update updates the progress and renders
func (p *Progress) Update(current int) {
	p.current = current
	p.render()
}

// Increment increments progress by 1
func (p *Progress) Increment() {
	p.current++
	p.render()
}

func (p *Progress) render() {
	// Accessible mode or non-TTY: print simple text progress
	if styles.IsAccessible() || !term.IsTerminal(int(os.Stdout.Fd())) {
		pct := p.current * 100 / p.total
		// Print every 10% to avoid spam
		if pct%10 == 0 && (p.current == 0 || (p.current-1)*100/p.total != pct) {
			fmt.Printf("%s: %d%% (%d of %d)\n", p.label, pct, p.current, p.total)
		}
		return
	}

	pct := float64(p.current) / float64(p.total)
	filled := int(pct * float64(p.width))
	empty := p.width - filled

	bar := lipgloss.NewStyle().Foreground(styles.Success).Render(
		repeatStr("█", filled),
	) + lipgloss.NewStyle().Foreground(styles.Muted).Render(
		repeatStr("░", empty),
	)

	fmt.Printf("\r%s %s %3d%% [%d/%d]", p.label, bar, int(pct*100), p.current, p.total)
}

// Done finishes the progress bar
func (p *Progress) Done() {
	p.current = p.total
	p.render()
	fmt.Println()
}

func repeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

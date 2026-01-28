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

// Progress represents a progress bar with ETA estimation
type Progress struct {
	total     int
	current   int
	label     string
	width     int
	startTime time.Time

	// ETA estimation using exponential moving average of rate
	lastUpdate time.Time
	lastCount  int
	emaRate    float64 // items per second (exponential moving average)
	emaAlpha   float64 // smoothing factor (0.1 = slow adapt, 0.5 = fast adapt)
	samples    int     // number of rate samples collected
}

// NewProgress creates a new progress bar
func NewProgress(label string, total int) *Progress {
	now := time.Now()
	return &Progress{
		label:      label,
		total:      total,
		width:      30,
		startTime:  now,
		lastUpdate: now,
		lastCount:  0,
		emaRate:    0,
		emaAlpha:   0.3, // balance between responsiveness and stability
		samples:    0,
	}
}

// Update updates the progress and renders
func (p *Progress) Update(current int) {
	now := time.Now()

	// Calculate instantaneous rate if enough time has passed
	elapsed := now.Sub(p.lastUpdate).Seconds()
	if elapsed >= 0.1 && current > p.lastCount { // at least 100ms between samples
		instantRate := float64(current-p.lastCount) / elapsed

		// Update EMA
		if p.samples == 0 {
			p.emaRate = instantRate
		} else {
			p.emaRate = p.emaAlpha*instantRate + (1-p.emaAlpha)*p.emaRate
		}
		p.samples++

		p.lastUpdate = now
		p.lastCount = current
	}

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
			eta := p.estimateETA()
			if eta != "" {
				fmt.Printf("%s: %d%% (%d of %d) ETA: %s\n", p.label, pct, p.current, p.total, eta)
			} else {
				fmt.Printf("%s: %d%% (%d of %d)\n", p.label, pct, p.current, p.total)
			}
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

	eta := p.estimateETA()
	if eta != "" {
		eta = " " + lipgloss.NewStyle().Foreground(styles.Muted).Render(eta)
	}

	fmt.Printf("\r%s %s %3d%% [%d/%d]%s", p.label, bar, int(pct*100), p.current, p.total, eta)
}

// estimateETA returns a human-readable ETA string
func (p *Progress) estimateETA() string {
	remaining := p.total - p.current
	if remaining <= 0 || p.samples < 3 || p.emaRate <= 0 {
		return ""
	}

	// Calculate remaining time based on EMA rate
	secondsRemaining := float64(remaining) / p.emaRate

	// Don't show ETA if it's unreasonably large (> 24 hours)
	if secondsRemaining > 86400 {
		return ""
	}

	return formatDuration(time.Duration(secondsRemaining * float64(time.Second)))
}

// formatDuration formats a duration as a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
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

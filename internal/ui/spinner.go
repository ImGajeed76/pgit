package ui

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"golang.org/x/term"
)

// Spinner provides a simple animated spinner for long operations
type Spinner struct {
	message string
	done    chan struct{}
	stopped bool
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
				fmt.Printf("\r\033[K%s %s", frame, s.message)
				i++
			}
		}
	}()
}

// Stop stops the spinner
func (s *Spinner) Stop() {
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.done)
	time.Sleep(20 * time.Millisecond) // Let the goroutine clean up
}

// Success stops the spinner and shows a success message
func (s *Spinner) Success(msg string) {
	s.Stop()
	fmt.Println(styles.SuccessMsg(msg))
}

// Error stops the spinner and shows an error message
func (s *Spinner) Error(msg string) {
	s.Stop()
	fmt.Println(styles.ErrorMsg(msg))
}

// ══════════════════════════════════════════════════════════════════════════
// Progress bar for operations with known progress
// ══════════════════════════════════════════════════════════════════════════

// rateSample stores a rate measurement with its progress position
type rateSample struct {
	progress float64 // 0.0 to 1.0
	rate     float64 // items per second
	time     float64 // seconds since start
}

// Progress represents a progress bar with rate and ETA estimation
// Uses curve fitting (quadratic regression) to predict future rates
// accounting for acceleration/deceleration patterns in the import
type Progress struct {
	mu        sync.Mutex
	label     string
	total     int
	current   int
	width     int
	startTime time.Time
	isTTY     bool

	// Rate estimation
	lastUpdate time.Time
	lastCount  int
	emaRate    float64 // items per second (recent, for display)
	samples    int

	// Historical samples for curve fitting
	// Stores (progress, rate) pairs to fit a curve
	rateSamples   []rateSample
	maxSamples    int
	sampleEvery   int // Only store every Nth sample to avoid memory bloat
	sampleCounter int

	// For non-TTY: track last printed percentage to avoid spam
	lastPrintedPct int
}

// NewProgress creates a new progress bar
func NewProgress(label string, total int) *Progress {
	now := time.Now()
	return &Progress{
		label:          label,
		total:          total,
		current:        0,
		width:          30,
		startTime:      now,
		isTTY:          term.IsTerminal(int(os.Stdout.Fd())) && !styles.IsAccessible(),
		lastUpdate:     now,
		lastCount:      0,
		emaRate:        0,
		samples:        0,
		rateSamples:    make([]rateSample, 0, 50),
		maxSamples:     50,
		sampleEvery:    3, // Store every 3rd sample
		sampleCounter:  0,
		lastPrintedPct: -1,
	}
}

// Update updates the progress and renders
func (p *Progress) Update(current int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()

	// Calculate instantaneous rate if enough time has passed
	elapsed := now.Sub(p.lastUpdate).Seconds()
	if elapsed >= 0.2 && current > p.lastCount {
		instantRate := float64(current-p.lastCount) / elapsed

		// Update EMA for display
		if p.samples == 0 {
			p.emaRate = instantRate
		} else {
			p.emaRate = 0.2*instantRate + 0.8*p.emaRate
		}
		p.samples++

		// Store sample for curve fitting (not every update to save memory)
		p.sampleCounter++
		if p.sampleCounter >= p.sampleEvery {
			p.sampleCounter = 0
			progress := float64(current) / float64(p.total)
			timeSinceStart := now.Sub(p.startTime).Seconds()

			p.rateSamples = append(p.rateSamples, rateSample{
				progress: progress,
				rate:     instantRate,
				time:     timeSinceStart,
			})

			// Trim old samples if too many
			if len(p.rateSamples) > p.maxSamples {
				p.rateSamples = p.rateSamples[1:]
			}
		}

		p.lastUpdate = now
		p.lastCount = current
	}

	p.current = current
	p.render()
}

// SetTotal updates the total (useful when discovered later)
func (p *Progress) SetTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
}

// estimateRemainingTime uses linear extrapolation based on elapsed time
func (p *Progress) estimateRemainingTime() float64 {
	if p.current >= p.total || p.current == 0 {
		return 0
	}

	elapsed := time.Since(p.startTime).Seconds()
	progress := float64(p.current) / float64(p.total)

	// Simple linear extrapolation: if we're X% done in T seconds,
	// total time = T / X, remaining = total - elapsed
	return (elapsed / progress) - elapsed
}

func (p *Progress) render() {
	if p.total <= 0 {
		return
	}

	pct := p.current * 100 / p.total

	// Non-TTY mode: print only at percentage milestones
	if !p.isTTY {
		milestone := (pct / 10) * 10 // Round down to nearest 10%
		if milestone > p.lastPrintedPct {
			p.lastPrintedPct = milestone
			rate := ""
			if p.emaRate > 0 {
				rate = fmt.Sprintf(" %.0f/s", p.emaRate)
			}
			eta := p.formatETA()
			if eta != "" {
				eta = " ETA " + eta
			}
			fmt.Printf("%s: %d%% [%d/%d]%s%s\n", p.label, pct, p.current, p.total, rate, eta)
		}
		return
	}

	// TTY mode: update in place
	pctFloat := float64(p.current) / float64(p.total)
	filled := int(pctFloat * float64(p.width))
	empty := p.width - filled

	bar := lipgloss.NewStyle().Foreground(styles.Success).Render(
		repeat("█", filled),
	) + lipgloss.NewStyle().Foreground(styles.Muted).Render(
		repeat("░", empty),
	)

	// Rate (show current EMA rate)
	rate := ""
	if p.emaRate > 0 {
		rate = fmt.Sprintf(" %.0f/s", p.emaRate)
	}

	// ETA using curve-fitted prediction
	eta := p.formatETA()
	if eta != "" {
		eta = " " + eta
	}

	suffix := lipgloss.NewStyle().Foreground(styles.Muted).Render(rate + eta)

	// \r = return to start, \033[K = clear to end of line
	fmt.Printf("\r\033[K%s %s %3d%% [%d/%d]%s", p.label, bar, pct, p.current, p.total, suffix)
}

func (p *Progress) formatETA() string {
	if p.current >= p.total || p.samples < 3 {
		return ""
	}

	secondsRemaining := p.estimateRemainingTime()
	if secondsRemaining <= 0 || secondsRemaining > 86400 { // > 24 hours
		return ""
	}

	return FormatDuration(time.Duration(secondsRemaining * float64(time.Second)))
}

// Done finishes the progress bar and shows elapsed time
func (p *Progress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	elapsed := time.Since(p.startTime)
	p.current = p.total

	// Log estimation stats for debugging/improvement
	p.logEstimationStats(elapsed)

	if p.isTTY {
		// Clear the progress line and show completion with elapsed time
		pctFloat := float64(p.current) / float64(p.total)
		filled := int(pctFloat * float64(p.width))
		empty := p.width - filled

		bar := lipgloss.NewStyle().Foreground(styles.Success).Render(
			repeat("█", filled),
		) + lipgloss.NewStyle().Foreground(styles.Muted).Render(
			repeat("░", empty),
		)

		elapsedStr := lipgloss.NewStyle().Foreground(styles.Muted).Render(
			fmt.Sprintf(" %s", FormatDuration(elapsed)),
		)

		fmt.Printf("\r\033[K%s %s 100%% [%d/%d]%s\n", p.label, bar, p.total, p.total, elapsedStr)
	} else if p.lastPrintedPct < 100 {
		fmt.Printf("%s: 100%% [%d/%d] done in %s\n", p.label, p.total, p.total, FormatDuration(elapsed))
	}
}

// logEstimationStats logs ETA estimation stats to a file for analysis
func (p *Progress) logEstimationStats(actualElapsed time.Duration) {
	// Only log if PGIT_LOG_ETA is set
	logPath := os.Getenv("PGIT_LOG_ETA")
	if logPath == "" {
		return
	}
	// If just "1" or "true", use default path in tmp
	if logPath == "1" || logPath == "true" {
		logPath = "/tmp/pgit_eta_stats.log"
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	avgRate := float64(p.total) / actualElapsed.Seconds()

	fmt.Fprintf(f, "\n=== %s: %s ===\n", p.label, time.Now().Format(time.RFC3339))
	fmt.Fprintf(f, "Total items: %d\n", p.total)
	fmt.Fprintf(f, "Actual elapsed: %.1fs\n", actualElapsed.Seconds())
	fmt.Fprintf(f, "Average rate: %.1f/s\n", avgRate)
	fmt.Fprintf(f, "Final EMA rate: %.1f/s\n", p.emaRate)

	// Log rate samples
	fmt.Fprintf(f, "\nRate samples (progress, rate, time):\n")
	for i, s := range p.rateSamples {
		fmt.Fprintf(f, "  [%2d] progress=%.3f rate=%.1f/s time=%.1fs\n", i, s.progress, s.rate, s.time)
	}

	// Calculate what linear ETA would have been at checkpoints
	fmt.Fprintf(f, "\nLinear ETA estimates at checkpoints vs actual remaining:\n")
	for _, checkpoint := range []float64{0.25, 0.50, 0.75, 0.90} {
		// Find the sample closest to this checkpoint
		var closestSample *rateSample
		for i := range p.rateSamples {
			if p.rateSamples[i].progress >= checkpoint {
				closestSample = &p.rateSamples[i]
				break
			}
		}
		if closestSample != nil {
			actualRemaining := actualElapsed.Seconds() - closestSample.time
			// Linear estimate: (elapsed / progress) - elapsed = elapsed * (1 - progress) / progress
			predictedRemaining := closestSample.time * (1.0 - closestSample.progress) / closestSample.progress
			errorPct := ((predictedRemaining - actualRemaining) / actualRemaining) * 100
			fmt.Fprintf(f, "  At %.0f%%: predicted=%.1fs actual=%.1fs error=%.1f%%\n",
				checkpoint*100, predictedRemaining, actualRemaining, errorPct)
		}
	}
	fmt.Fprintf(f, "\n")
}

// ══════════════════════════════════════════════════════════════════════════
// Utility functions
// ══════════════════════════════════════════════════════════════════════════

// FormatDuration formats a duration as a human-readable string
func FormatDuration(d time.Duration) string {
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

// FormatCount formats a count with thousand separators
func FormatCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d,%03d,%03d", n/1000000, (n/1000)%1000, n%1000)
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]byte, len(s)*n)
	for i := 0; i < n; i++ {
		copy(result[i*len(s):], s)
	}
	return string(result)
}

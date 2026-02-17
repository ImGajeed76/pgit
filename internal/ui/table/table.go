// Package table provides a reusable interactive table viewer and output
// formatters. It supports an interactive TUI (with search, column
// expand/hide, smooth scrolling), plain text tables, JSON output, and
// raw tab-separated output.
//
// This package is used by both `pgit sql` and `pgit analyze` to display
// tabular results.
package table

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// DisplayOptions controls how results are rendered.
type DisplayOptions struct {
	// JSON outputs results as a JSON array of objects.
	JSON bool
	// Raw outputs results as tab-separated values (for piping).
	Raw bool
	// NoPager forces plain table output even on a TTY.
	NoPager bool
}

// DisplayResults picks the right output mode based on options and environment,
// then renders the given columns and rows. The title is shown in the
// interactive TUI header; for non-interactive modes it is ignored.
func DisplayResults(title string, columns []string, rows [][]string, opts DisplayOptions) error {
	if opts.Raw {
		for _, row := range rows {
			fmt.Println(strings.Join(row, "\t"))
		}
		return nil
	}

	if opts.JSON {
		return PrintJSONResults(columns, rows)
	}

	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	if !isTTY || opts.NoPager || len(rows) == 0 {
		PrintPlainTable(columns, rows)
		return nil
	}

	return RunTableTUI(title, columns, rows)
}

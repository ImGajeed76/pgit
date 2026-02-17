package table

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// PrintJSONResults outputs results as a JSON array of objects.
func PrintJSONResults(colNames []string, rows [][]string) error {
	results := make([]map[string]interface{}, len(rows))

	for i, row := range rows {
		obj := make(map[string]interface{})
		for j, colName := range colNames {
			if j < len(row) {
				val := row[j]
				if val == "NULL" {
					obj[colName] = nil
				} else {
					obj[colName] = val
				}
			} else {
				obj[colName] = nil
			}
		}
		results[i] = obj
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// PrintPlainTable prints a properly aligned table for non-TTY output.
// Shows full content without truncation.
func PrintPlainTable(colNames []string, rows [][]string) {
	if len(colNames) == 0 {
		fmt.Println("(0 rows)")
		return
	}

	// Calculate column widths based on actual content (no truncation)
	colWidths := make([]int, len(colNames))
	for i, name := range colNames {
		colWidths[i] = len(name)
	}
	for _, row := range rows {
		for i, val := range row {
			if i < len(colWidths) && len(val) > colWidths[i] {
				colWidths[i] = len(val)
			}
		}
	}

	// Print header
	for i, name := range colNames {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Print(pad(name, colWidths[i]))
	}
	fmt.Println()

	// Print separator
	for i, w := range colWidths {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Print(strings.Repeat("─", w))
	}
	fmt.Println()

	// Print rows (full content, no truncation)
	for _, row := range rows {
		for i, val := range row {
			if i >= len(colWidths) {
				break
			}
			if i > 0 {
				fmt.Print("  ")
			}
			fmt.Print(pad(val, colWidths[i]))
		}
		fmt.Println()
	}

	fmt.Println()
	fmt.Printf("(%d rows)\n", len(rows))
}

// pad adds spaces to reach the desired width (no truncation).
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// Truncate shortens a string to fit width, adding "..." if needed.
func Truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width > 3 {
		return s[:width-3] + "..."
	}
	return s[:width]
}

// PadOrTruncate pads or truncates to exact width (for TUI table).
func PadOrTruncate(s string, width int) string {
	if len(s) > width {
		return Truncate(s, width)
	}
	return s + strings.Repeat(" ", width-len(s))
}

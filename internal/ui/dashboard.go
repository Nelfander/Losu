package ui

import (
	"fmt"
	"strings"

	"github.com/nelfander/losu/internal/model"
)

// Render draws the dashboard at the top of the terminal
func Render(snap model.Snapshot) {
	// \033[H moves cursor to top-left
	// \033[J clears the screen from cursor down
	fmt.Print("\033[H\033[J")

	fmt.Println("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓")
	fmt.Println("┃             LOSU REAL-TIME MONITOR             ┃")
	fmt.Println("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛")
	fmt.Printf("  Total Logs Processed: %d\n", snap.TotalLines)
	fmt.Println("  Breakdown:")

	for level, count := range snap.ErrorCounts {
		color := "\033[37m" // White
		if level == "ERROR" {
			color = "\033[31m" // Red
		} else if level == "INFO" {
			color = "\033[32m" // Green
		}
		fmt.Printf("    %s%-10s\033[0m : %d\n", color, level, count)
	}
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println("  Latest Logs:")
}

// input.go — keyboard and mouse capture handlers for the LOSU TUI.
//
// setupInput() is called once from NewDashboard() and wires up all
// interactive behaviour: mouse drag/scroll on logs and top errors,
// global hotkeys (Ctrl+C), stats file-cycling, and the full Level 1 /
// Level 2 inspector flow triggered by Enter on a highlighted row.
// openTopErrorsFullscreen() and openInspectorL1() are extracted helpers
// to keep setupInput() readable.
package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// setupInput wires all keyboard and mouse handlers onto the provided
// tview primitives. Called once from NewDashboard().
func (d *Dashboard) setupInput(
	app *tview.Application,
	stats *tview.TextView,
	topErrors *tview.TextView,
	logs *tview.TextView,
	searchInput *tview.InputField,
	ai *tview.TextView,
	graph *tview.TextView,
) {
	// ── Global: Ctrl+C quits, / jumps to search ────────────────────────────
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		// '/' from anywhere focuses the search box
		if event.Rune() == '/' {
			app.SetFocus(searchInput)
			return nil
		}
		return event
	})

	// ── Search input: Esc clears filter ──────────────────────────────────────
	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			searchInput.SetText("")
			d.SearchFilter = ""
		}
	})

	// ── Stats panel: Tab → Top Errors, Left/Right cycles log files ──────────
	stats.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(topErrors)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			app.SetFocus(searchInput)
			return nil
		}
		if len(d.AggKeys) > 1 {
			currentIdx := 0
			for i, key := range d.AggKeys {
				if d.AggMap[key] == d.ActiveAgg {
					currentIdx = i
					break
				}
			}
			if event.Key() == tcell.KeyRight {
				nextIdx := (currentIdx + 1) % len(d.AggKeys)
				d.ActiveAgg = d.AggMap[d.AggKeys[nextIdx]]
				d.FilteredLogs = d.FilteredLogs[:0]
				d.LastHistoryLen = 0
				return nil
			}
			if event.Key() == tcell.KeyLeft {
				prevIdx := (currentIdx - 1 + len(d.AggKeys)) % len(d.AggKeys)
				d.ActiveAgg = d.AggMap[d.AggKeys[prevIdx]]
				d.FilteredLogs = d.FilteredLogs[:0]
				d.LastHistoryLen = 0
				return nil
			}
		}
		return event
	})

	// ── Mouse: drag-to-scroll on Latest Logs ─────────────────────────────────
	logs.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := logs.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			if x >= scrollbarX-1 || d.isDragging {
				d.isDragging = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := len(d.FilteredLogs)
				targetLine := int(percentage * float64(totalLines))
				logs.ScrollTo(targetLine, 0)
				d.isAutoScroll = (percentage >= 0.95)
				return action, nil
			}
		} else {
			d.isDragging = false
		}
		return action, event
	})

	// ── Mouse: accelerated wheel + drag on Top Errors/Warns ──────────────────
	var isDraggingTop bool
	var lastTopScrollTime time.Time
	var topScrollAccel int

	topErrors.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := topErrors.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
			now := time.Now()
			since := now.Sub(lastTopScrollTime).Milliseconds()
			lastTopScrollTime = now
			if since < 120 {
				topScrollAccel++
				if topScrollAccel > 4 {
					topScrollAccel = 4
				}
			} else {
				topScrollAccel = 0
			}
			jump := 3 + topScrollAccel*3
			offset, _ := topErrors.GetScrollOffset()
			if action == tview.MouseScrollDown {
				topErrors.ScrollTo(offset+jump, 0)
			} else {
				next := offset - jump
				if next < 0 {
					next = 0
				}
				topErrors.ScrollTo(next, 0)
			}
			return action, nil
		}

		if leftPressed {
			if x >= scrollbarX-1 || isDraggingTop {
				isDraggingTop = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := strings.Count(topErrors.GetText(false), "\n")
				targetLine := int(percentage * float64(totalLines))
				topErrors.ScrollTo(targetLine, 0)
				return action, nil
			}
		} else {
			isDraggingTop = false
		}
		return action, event
	})

	// ── Keyboard: Top Errors — navigation + fullscreen + inspector ───────────
	topErrors.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'f' {
			d.openTopErrorsFullscreen(app, topErrors)
			return nil
		}
		if event.Key() == tcell.KeyEnter {
			d.openInspectorL1(app, topErrors)
			return nil
		}
		if event.Key() == tcell.KeyTab {
			app.SetFocus(logs)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			app.SetFocus(stats)
			return nil
		}
		// Up/Down — move highlight between rows
		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown {
			total := len(d.StatLookup)
			if total == 0 {
				return nil
			}
			highlights := topErrors.GetHighlights()
			current := 0
			if len(highlights) > 0 {
				fmt.Sscanf(highlights[0], "top_%d", &current)
			}
			if event.Key() == tcell.KeyDown {
				current++
				if current >= total {
					current = total - 1
				}
			} else {
				current--
				if current < 0 {
					current = 0
				}
			}
			topErrors.Highlight(fmt.Sprintf("top_%d", current))
			topErrors.ScrollToHighlight()
			return nil
		}
		// Page Down/Up — fast scroll
		if event.Key() == tcell.KeyPgDn {
			offset, _ := topErrors.GetScrollOffset()
			topErrors.ScrollTo(offset+10, 0)
			return nil
		}
		if event.Key() == tcell.KeyPgUp {
			offset, _ := topErrors.GetScrollOffset()
			next := offset - 10
			if next < 0 {
				next = 0
			}
			topErrors.ScrollTo(next, 0)
			return nil
		}
		return event
	})

	// ── Keyboard: Logs — Tab → Search, Shift+Tab → Top Errors, PgUp/Dn ──────
	logs.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(searchInput)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			app.SetFocus(topErrors)
			return nil
		}
		if event.Key() == tcell.KeyPgDn {
			offset, _ := logs.GetScrollOffset()
			logs.ScrollTo(offset+20, 0)
			return nil
		}
		if event.Key() == tcell.KeyPgUp {
			offset, _ := logs.GetScrollOffset()
			next := offset - 20
			if next < 0 {
				next = 0
			}
			logs.ScrollTo(next, 0)
			return nil
		}
		return event
	})

	// ── Keyboard: Search — Tab → Stats, Shift+Tab → Logs ─────────────────────
	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(stats)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			app.SetFocus(logs)
			return nil
		}
		return event
	})

	// ── Keyboard: AI + Graph — pass Tab through so focus always cycles ────────
	// These panels are read-only. Tab moves to Stats, Shift+Tab to Search.
	for _, panel := range []*tview.TextView{ai, graph} {
		p := panel
		p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyTab {
				app.SetFocus(stats)
				return nil
			}
			if event.Key() == tcell.KeyBacktab {
				app.SetFocus(searchInput)
				return nil
			}
			return event
		})
	}
}

// openTopErrorsFullscreen renders a full-terminal overlay of all error/warn
// patterns at 150-char truncation. f, Esc, or q closes it.
// Enter on a highlighted row delegates to topErrors to open the Level 1 inspector.
func (d *Dashboard) openTopErrorsFullscreen(app *tview.Application, topErrors *tview.TextView) {
	pages := d.Pages

	var fsContent strings.Builder
	fsContent.WriteString("\n")
	for i, item := range d.StatLookup {
		bestMsg := ""
		maxSubCount := -1
		for msg, count := range item.VariantCounts {
			if count > maxSubCount {
				maxSubCount = count
				bestMsg = msg
			} else if count == maxSubCount && msg < bestMsg {
				bestMsg = msg
			}
		}
		color := "red"
		if item.Level == "WARN" {
			color = "yellow"
		}
		fsContent.WriteString(fmt.Sprintf(" [\"top_%d\"][%s]%5d [white]| %-150s[\"\"]\n",
			i, color, item.Count, truncate(bestMsg, 150)))
	}

	fsView := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetScrollable(true).
		SetText(fsContent.String())
	fsView.SetBorder(true).
		SetTitle(" [red]Top Errors / Warns [gray]— fullscreen  |  f/Esc: close  |  ↑↓: navigate  |  Enter: inspect  |  PgUp/Dn: scroll ")

	var fsLastScroll time.Time
	var fsAccel int
	fsView.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := fsView.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
			now := time.Now()
			since := now.Sub(fsLastScroll).Milliseconds()
			fsLastScroll = now
			if since < 120 {
				fsAccel++
				if fsAccel > 4 {
					fsAccel = 4
				}
			} else {
				fsAccel = 0
			}
			jump := 3 + fsAccel*3
			offset, _ := fsView.GetScrollOffset()
			if action == tview.MouseScrollDown {
				fsView.ScrollTo(offset+jump, 0)
			} else {
				next := offset - jump
				if next < 0 {
					next = 0
				}
				fsView.ScrollTo(next, 0)
			}
			return action, nil
		}

		if leftPressed && (x >= scrollbarX-1) {
			relativeY := float64(y - rectY)
			percentage := relativeY / float64(rectHeight)
			if percentage < 0 {
				percentage = 0
			}
			if percentage > 1 {
				percentage = 1
			}
			totalLines := strings.Count(fsView.GetText(false), "\n")
			targetLine := int(percentage * float64(totalLines))
			fsView.ScrollTo(targetLine, 0)
			return action, nil
		}
		return action, event
	})

	fsView.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Rune() == 'f' || ev.Rune() == 'q' {
			pages.RemovePage("top-fullscreen")
			app.SetFocus(topErrors)
			return nil
		}
		if ev.Key() == tcell.KeyEnter {
			highlights := fsView.GetHighlights()
			if len(highlights) > 0 {
				topErrors.Highlight(highlights[0])
				pages.RemovePage("top-fullscreen")
				app.SetFocus(topErrors)
				app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			}
			return nil
		}
		// Up/Down — navigate rows in fullscreen view
		if ev.Key() == tcell.KeyUp || ev.Key() == tcell.KeyDown {
			total := len(d.StatLookup)
			if total == 0 {
				return nil
			}
			highlights := fsView.GetHighlights()
			current := 0
			if len(highlights) > 0 {
				fmt.Sscanf(highlights[0], "top_%d", &current)
			}
			if ev.Key() == tcell.KeyDown {
				current++
				if current >= total {
					current = total - 1
				}
			} else {
				current--
				if current < 0 {
					current = 0
				}
			}
			fsView.Highlight(fmt.Sprintf("top_%d", current))
			fsView.ScrollToHighlight()
			return nil
		}
		// Page Down/Up — fast scroll
		if ev.Key() == tcell.KeyPgDn {
			offset, _ := fsView.GetScrollOffset()
			fsView.ScrollTo(offset+10, 0)
			return nil
		}
		if ev.Key() == tcell.KeyPgUp {
			offset, _ := fsView.GetScrollOffset()
			next := offset - 10
			if next < 0 {
				next = 0
			}
			fsView.ScrollTo(next, 0)
			return nil
		}
		return ev
	})

	fsView.SetHighlightedFunc(func(added, removed, remaining []string) {
		if len(added) > 0 {
			topErrors.Highlight(added[0])
		}
	})

	pages.AddPage("top-fullscreen", fsView, true, true)
	app.SetFocus(fsView)
}

// openInspectorL1 opens the Level 1 variant list for the currently highlighted
// top error/warn row. Each row shows last-hit time, count, and message.
// Enter on a variant opens the Level 2 timeline inspector.
func (d *Dashboard) openInspectorL1(app *tview.Application, topErrors *tview.TextView) {
	pages := d.Pages

	highlights := topErrors.GetHighlights()
	if len(highlights) == 0 {
		return
	}

	var index int
	fmt.Sscanf(highlights[0], "top_%d", &index)
	if index < 0 || index >= len(d.StatLookup) {
		return
	}

	stat := d.StatLookup[index]
	levelColor := d.getColor(stat.Level)

	type varEntry struct {
		msg        string
		count      int
		lastHit    time.Time
		hasLastHit bool
	}

	varList := make([]varEntry, 0, len(stat.VariantCounts))
	for msg, count := range stat.VariantCounts {
		entry := varEntry{msg: msg, count: count}
		if stat.VariantTimestamps != nil {
			if vt, ok := stat.VariantTimestamps[msg]; ok {
				ordered := vt.Slice()
				if len(ordered) > 0 {
					entry.lastHit = ordered[len(ordered)-1]
					entry.hasLastHit = true
				}
			}
		}
		varList = append(varList, entry)
	}

	sort.Slice(varList, func(i, j int) bool {
		if varList[i].count != varList[j].count {
			return varList[i].count > varList[j].count
		}
		return varList[i].lastHit.After(varList[j].lastHit)
	})

	list := tview.NewList()
	list.SetBorder(true)
	list.SetTitle(fmt.Sprintf(" [%s]%s [white]— %d variants | Enter: timeline  Esc: back ",
		levelColor, tview.Escape(stat.Level), len(varList)))
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedBackgroundColor(tcell.ColorDarkSlateGray)

	var lastScrollTime time.Time
	var scrollAccel int
	list.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action != tview.MouseScrollUp && action != tview.MouseScrollDown {
			return action, event
		}
		now := time.Now()
		since := now.Sub(lastScrollTime).Milliseconds()
		lastScrollTime = now
		if since < 120 {
			scrollAccel++
			if scrollAccel > 4 {
				scrollAccel = 4
			}
		} else {
			scrollAccel = 0
		}
		jump := 3 + scrollAccel*3
		current := list.GetCurrentItem()
		count := list.GetItemCount()
		if action == tview.MouseScrollDown {
			next := current + jump
			if next >= count {
				next = count - 1
			}
			list.SetCurrentItem(next)
		} else {
			next := current - jump
			if next < 0 {
				next = 0
			}
			list.SetCurrentItem(next)
		}
		return action, nil
	})

	for _, ve := range varList {
		timeStr := "    —        "
		if ve.hasLastHit {
			timeStr = ve.lastHit.Format("15:04:05.000")
		}
		label := fmt.Sprintf("[gray]%s  [%s]%-4d[-]  %s",
			timeStr, levelColor, ve.count, tview.Escape(truncate(ve.msg, 80)))
		list.AddItem(label, ve.msg, 0, nil)
	}

	headerText := tview.NewTextView().
		SetDynamicColors(true).
		SetText(fmt.Sprintf("[gray] %-13s  %-4s  %s\n%s",
			"LAST HIT", "HITS", "MESSAGE",
			strings.Repeat("─", 80),
		))

	modalInner := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(headerText, 2, 0, false).
		AddItem(list, 0, 1, true)

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modalInner, 0, 5, true).
			AddItem(nil, 0, 1, false), 0, 3, true).
		AddItem(nil, 0, 1, false)

	pages.AddPage("inspector-l1", modal, true, true)
	app.SetFocus(list)

	list.SetSelectedFunc(func(i int, mainText, secondaryText string, r rune) {
		varMsg := secondaryText
		var varTimestamps []time.Time
		if stat.VariantTimestamps != nil {
			if vt, ok := stat.VariantTimestamps[varMsg]; ok {
				ordered := vt.Slice()
				for idx := len(ordered) - 1; idx >= 0; idx-- {
					if !ordered[idx].IsZero() {
						varTimestamps = append(varTimestamps, ordered[idx])
					}
				}
			}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[%s]%s\n", levelColor, tview.Escape(varMsg)))
		sb.WriteString(fmt.Sprintf("[gray]%s\n\n", strings.Repeat("━", 64)))
		sb.WriteString(fmt.Sprintf("[white]Total hits for this variant: [%s]%d\n\n",
			levelColor, stat.VariantCounts[varMsg]))

		if len(varTimestamps) == 0 {
			sb.WriteString("[gray]No timestamp data yet — keep running to populate.\n")
		} else {
			sb.WriteString(fmt.Sprintf("[cyan]🕒 Hit Timeline (last %d, newest first):\n\n", len(varTimestamps)))
			for _, ts := range varTimestamps {
				diff := time.Since(ts)
				var diffStr string
				switch {
				case diff < time.Second:
					diffStr = "< 1s ago"
				case diff < time.Minute:
					diffStr = fmt.Sprintf("%ds ago", int(diff.Seconds()))
				case diff < time.Hour:
					diffStr = fmt.Sprintf("%dm %ds ago", int(diff.Minutes()), int(diff.Seconds())%60)
				default:
					diffStr = fmt.Sprintf("%dh ago", int(diff.Hours()))
				}
				sb.WriteString(fmt.Sprintf(" [white]%s  [gray](%s)\n", ts.Format("15:04:05.000"), diffStr))
			}
		}

		d.ShowVariantInspector(
			fmt.Sprintf("[%s]Variant Timeline[-]", levelColor),
			sb.String(),
			list,
		)
	})

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Rune() == 'q' {
			pages.RemovePage("inspector-l1")
			app.SetFocus(topErrors)
			return nil
		}
		return ev
	})
}
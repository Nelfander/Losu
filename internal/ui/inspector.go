// inspector.go — forensic drill-down popups for the LOSU TUI.
//
// ShowVariantInspector() opens the Level 2 timeline popup for a single
// variant, returning focus to the Level 1 list on Esc.
// ShowInspector() is the legacy single-level popup kept for backward
// compatibility with any direct callers outside the two-level flow.
// GetSummaryForAI() extracts the top 3 errors and top 3 warns from a
// recent snapshot for inclusion in the AI observer prompt.
package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelfander/losu/internal/model"
	"github.com/rivo/tview"
)

// ShowVariantInspector opens the Level 2 timeline popup for a specific variant.
// Esc returns focus to the Level 1 list (returnTo) instead of the top errors panel.
func (d *Dashboard) ShowVariantInspector(title, content string, returnTo tview.Primitive) {
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText("\n " + content)

	updateTitle := func() {
		offset, _ := textView.GetScrollOffset()
		_, _, _, rectHeight := textView.GetInnerRect()
		lines := strings.Split(content, "\n")
		totalLines := len(lines)
		if totalLines > rectHeight {
			maxScroll := totalLines - rectHeight
			if maxScroll < 1 {
				maxScroll = 1
			}
			percent := (float64(offset) / float64(maxScroll)) * 100
			if percent > 100 {
				percent = 100
			}
			textView.SetTitle(fmt.Sprintf(" %s [white]| %d%% ", title, int(percent)))
		} else {
			textView.SetTitle(fmt.Sprintf(" %s [white]| TOP ", title))
		}
	}

	updateTitle()
	textView.SetBorder(true)

	var isDragging bool
	textView.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := textView.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			if x >= scrollbarX-1 || isDragging {
				isDragging = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := strings.Count(textView.GetText(false), "\n")
				targetLine := int(percentage * float64(totalLines))
				textView.ScrollTo(targetLine, 0)
				updateTitle()
				return action, nil
			}
		} else {
			isDragging = false
		}
		if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
			defer updateTitle()
		}
		return action, event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(textView, 0, 4, true).
			AddItem(nil, 0, 1, false), 100, 1, true).
		AddItem(nil, 0, 1, false)

	d.Pages.AddPage("inspector-l2", modal, true, true)
	d.App.SetFocus(textView)

	// Esc → back to Level 1 list
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			d.Pages.RemovePage("inspector-l2")
			d.App.SetFocus(returnTo)
			return nil
		}
		return event
	})
}

// ShowInspector opens a scrollable popup with detailed stats.
// Kept for backward compatibility — used by direct callers outside the two-level flow.
func (d *Dashboard) ShowInspector(title, content string) {
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText("\n " + content)

	updatePopupTitle := func() {
		offset, _ := textView.GetScrollOffset()
		_, _, _, rectHeight := textView.GetInnerRect()
		lines := strings.Split(content, "\n")
		totalLines := len(lines)
		if totalLines > rectHeight {
			maxScroll := totalLines - rectHeight
			percent := (float64(offset) / float64(maxScroll)) * 100
			if percent > 100 {
				percent = 100
			}
			textView.SetTitle(fmt.Sprintf(" %s [white]| %d%% ", title, int(percent)))
		} else {
			textView.SetTitle(fmt.Sprintf(" %s [white]| TOP ", title))
		}
	}

	updatePopupTitle()
	textView.SetBorder(true)

	var isDraggingInspector bool
	textView.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := event.Position()
		rectX, rectY, rectWidth, rectHeight := textView.GetInnerRect()
		scrollbarX := rectX + rectWidth - 1
		leftPressed := event.Buttons()&tcell.Button1 != 0

		if leftPressed {
			if x >= scrollbarX-1 || isDraggingInspector {
				isDraggingInspector = true
				relativeY := float64(y - rectY)
				percentage := relativeY / float64(rectHeight)
				if percentage < 0 {
					percentage = 0
				}
				if percentage > 1 {
					percentage = 1
				}
				totalLines := strings.Count(textView.GetText(false), "\n")
				targetLine := int(percentage * float64(totalLines))
				textView.ScrollTo(targetLine, 0)
				updatePopupTitle()
				return action, nil
			}
		} else {
			isDraggingInspector = false
		}
		if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
			defer updatePopupTitle()
		}
		return action, event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(textView, 0, 4, true).
			AddItem(nil, 0, 1, false), 100, 1, true).
		AddItem(nil, 0, 1, false)

	d.Pages.AddPage("inspector", modal, true, true)
	d.App.SetFocus(textView)

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			d.Pages.RemovePage("inspector")
			d.App.SetFocus(d.TopErrorsView)
			return nil
		}
		return event
	})
}

// GetSummaryForAI gathers top 3 errors and top 3 warns from a recent snapshot
// for inclusion in the AI observer prompt.
func (d *Dashboard) GetSummaryForAI(snap model.Snapshot) (errors string, warns string) {
	type kv struct {
		Key  string
		Stat model.MessageStat
	}

	var sorted []kv
	for k, v := range snap.RecentMessages {
		sorted = append(sorted, kv{k, v})
	}

	if len(sorted) == 0 {
		return "No new errors reported in this window.", "No new warnings."
	}

	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Stat.Count > sorted[j].Stat.Count })

	var errB, warnB strings.Builder
	eCount, wCount := 0, 0

	for _, item := range sorted {
		bestMsg := ""
		max := -1
		for msg, count := range item.Stat.VariantCounts {
			if count > max {
				max = count
				bestMsg = msg
			}
		}
		if item.Stat.Level == "ERROR" && eCount < 3 {
			errB.WriteString(fmt.Sprintf("- %s (%d hits)\n", bestMsg, item.Stat.Count))
			eCount++
		} else if item.Stat.Level == "WARN" && wCount < 3 {
			warnB.WriteString(fmt.Sprintf("- %s (%d hits)\n", bestMsg, item.Stat.Count))
			wCount++
		}
	}
	return errB.String(), warnB.String()
}

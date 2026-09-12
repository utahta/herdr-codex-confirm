package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/utahta/herdr-codex-confirm/internal/confirm"
)

func ready() model {
	m := newModel(confirm.Request{Command: "git push", Cwd: "/tmp", Rules: [][]string{{"git", "push"}}, Deadline: time.Now().Add(time.Minute)})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(model)
}

func TestExplicitApproval(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc, tea.KeyCtrlC} {
		m := ready()
		next, _ := m.Update(tea.KeyMsg{Type: key})
		if next.(model).result.Approved || !next.(model).done {
			t.Errorf("%v did not deny", key)
		}
	}
	m := ready()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.(model).result.Approved {
		t.Fatal("explicit approval did not work")
	}
	m = ready()
	m.request.Deadline = time.Now().Add(-time.Second)
	m.selected = approveAction
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(model).result.Approved {
		t.Fatal("approved after deadline")
	}
}

func TestMatchedPatternsDisplay(t *testing.T) {
	m := ready()
	m.request.Rules = [][]string{{"git", "commit|push"}, {"gh", "pr", "create|merge"}}
	content := m.content()
	if !strings.Contains(content, "[\"git\",\"commit|push\"]\n[\"gh\",\"pr\",\"create|merge\"]") {
		t.Fatalf("missing word patterns: %s", content)
	}
}

func TestCompactCommandFirstLayout(t *testing.T) {
	m := ready()
	m.request.SourcePane = "workspace:pane"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = next.(model)
	view := ansi.Strip(m.View())
	previous := -1
	for _, text := range []string{"Run this command?", "git push", "Working directory: /tmp", "Details: matched", "Source pane: workspace:pane", "▶ Deny", "Expires in"} {
		index := strings.Index(view, text)
		if index <= previous {
			t.Fatalf("missing or misplaced %q in:\n%s", text, view)
		}
		previous = index
	}
	if height := strings.Count(view, "\n") + 1; height > 16 {
		t.Fatalf("short request leaves too much vertical space: %d lines\n%s", height, view)
	}
	if strings.Contains(view, "scroll") {
		t.Fatal("short request shows unnecessary scroll instructions")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	view = ansi.Strip(next.(model).View())
	if !strings.Contains(view, "▶ Deny with feedback") {
		t.Fatalf("selection is not visible without color:\n%s", view)
	}
}

func TestLayoutFitsTerminal(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 40, Height: 14}, {Width: 87, Height: 16}, {Width: 120, Height: 40}} {
		for _, command := range []string{"git push", "git push " + strings.Repeat("日本語 branch ", 300)} {
			m := ready()
			m.request.Command = command
			m.request.Cwd = "/tmp/" + strings.Repeat("long-directory/", 10)
			m.request.SourcePane = "workspace:pane"
			next, _ := m.Update(size)
			view := ansi.Strip(next.(model).View())
			if height := strings.Count(view, "\n") + 1; height > size.Height {
				t.Fatalf("%dx%d: view is %d lines:\n%s", size.Width, size.Height, height, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if width := ansi.StringWidth(line); width > size.Width {
					t.Fatalf("%dx%d: line is %d columns: %q", size.Width, size.Height, width, line)
				}
			}
			for _, text := range []string{"Run this command?", "▶ Deny", "Deny with feedback", "Approve once", "Expires in"} {
				if !strings.Contains(view, text) {
					t.Fatalf("%dx%d: missing %q:\n%s", size.Width, size.Height, text, view)
				}
			}
			if len(command) > 1000 && !strings.Contains(view, "scroll") {
				t.Fatalf("%dx%d: long command has no scroll instructions", size.Width, size.Height)
			}
		}
	}
}

func TestResizeResetsSelectionAndScroll(t *testing.T) {
	m := ready()
	m.request.Command = "git push " + strings.Repeat("branch ", 60)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 36, Height: 12})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnd})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
	next, _ = next.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(model)
	if m.selected != denyAction || m.viewport.YOffset != 0 || !strings.Contains(ansi.Strip(m.View()), "git push") {
		t.Fatalf("resize lost the command or kept approval selected: %+v", m)
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 35, Height: 12}, {Width: 36, Height: 11}} {
		next, _ := m.Update(size)
		next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
		next, _ = next.Update(tea.KeyMsg{Type: tea.KeyTab})
		next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if next.(model).result.Approved || next.(model).done {
			t.Fatalf("%dx%d: accepted input without enough room to review", size.Width, size.Height)
		}
	}
}

func TestLongCommandAndControlCharacters(t *testing.T) {
	m := ready()
	m.request.Command = strings.Repeat("a", 12000) + "FINAL"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	if strings.Contains(m.View(), "FINAL") {
		t.Fatal("unexpected initial viewport")
	}
	m = typeText(m, "j")
	if m.viewport.YOffset != 1 {
		t.Fatal("single-character scroll key stopped working")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	if !strings.Contains(m.View(), "FINAL") {
		t.Fatal("cannot view end of long command")
	}
	unsafe := "normal\x1b[2J\r\b\x07\u202e\u009b\t日本語\nend"
	safe := Safe(unsafe)
	if strings.ContainsAny(safe, "\x1b\r\b\x07\u202e\u009b\t") || !strings.Contains(safe, "日本語\nend") {
		t.Fatalf("unsafe display %q", safe)
	}
}

func key(m model, typ tea.KeyType) model {
	next, _ := m.Update(tea.KeyMsg{Type: typ})
	next.View()
	return next.(model)
}

func typeText(m model, text string) model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	next.View()
	return next.(model)
}

func editing() model {
	return key(key(ready(), tea.KeyTab), tea.KeyEnter)
}

func TestActionNavigation(t *testing.T) {
	for _, tc := range []struct {
		key   tea.KeyType
		order []int
	}{
		{tea.KeyTab, []int{feedbackAction, approveAction, denyAction}},
		{tea.KeyRight, []int{feedbackAction, approveAction, denyAction}},
		{tea.KeyShiftTab, []int{approveAction, feedbackAction, denyAction}},
		{tea.KeyLeft, []int{approveAction, feedbackAction, denyAction}},
	} {
		m := ready()
		for _, want := range tc.order {
			m = key(m, tc.key)
			if m.selected != want || m.done {
				t.Fatalf("%v: selected %d, want %d", tc.key, m.selected, want)
			}
			if !strings.Contains(ansi.Strip(m.View()), "▶ "+actionLabels[want]) {
				t.Fatalf("%v: selection not visible", tc.key)
			}
		}
	}
}

func TestFeedbackEditingAndSubmission(t *testing.T) {
	m := editing()
	if !m.editing || m.done || !m.feedback.Focused() {
		t.Fatal("feedback action did not focus the input")
	}
	m = typeText(m, "先にテストを追加してください。")
	m = key(m, tea.KeyEnter)
	m = typeText(m, "q")
	m = typeText(m, "n")
	m = typeText(m, " を含むケースも確認してください。")
	if m.done || !m.editing {
		t.Fatal("text editing or Enter submitted the feedback")
	}
	want := "先にテストを追加してください。\nqn を含むケースも確認してください。"
	if m.feedback.Value() != want || m.result != (confirm.Result{}) {
		t.Fatalf("unexpected draft or result: %q, %+v", m.feedback.Value(), m.result)
	}
	m = key(m, tea.KeyCtrlS)
	if !m.done || m.result.Approved || m.result.Feedback != want {
		t.Fatalf("feedback submission changed: %+v", m.result)
	}
	if m.View() != "" {
		t.Fatal("submitted feedback remains on screen")
	}
}

func TestLiteralKeyNamesInFeedback(t *testing.T) {
	for _, text := range []string{"ctrl+s", "ctrl+c", "esc", "enter", "tab", "ctrl+u", "ctrl+v", "home", "end", "right", "alt+f", "q", "n"} {
		for _, paste := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/paste=%t", text, paste), func(t *testing.T) {
				m := typeText(editing(), "draft: ")
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: paste})
				m = next.(model)
				want := "draft: " + text
				if m.done || !m.editing || m.result != (confirm.Result{}) || m.feedback.Value() != want {
					t.Fatalf("literal %q acted as a shortcut: editing=%t done=%t feedback=%q result=%+v", text, m.editing, m.done, m.feedback.Value(), m.result)
				}
				m = key(m, tea.KeyCtrlS)
				if !m.done || m.result.Approved || m.result.Feedback != want {
					t.Fatalf("literal text was not submitted intact: %+v", m.result)
				}
			})
		}
	}
}

func TestLiteralKeyNamesDoNotChangeSelection(t *testing.T) {
	for _, text := range []string{"ctrl+c", "ctrl+s", "esc", "enter", "tab", "shift+tab", "left", "right", "home", "end", "down"} {
		m := key(key(ready(), tea.KeyTab), tea.KeyTab)
		m = typeText(m, text)
		if m.done || m.editing || m.selected != approveAction || m.result != (confirm.Result{}) {
			t.Fatalf("literal %q changed the decision", text)
		}
	}
	for _, text := range []string{"q", "n"} {
		for _, paste := range []bool{false, true} {
			for _, alt := range []bool{false, true} {
				next, _ := ready().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: paste, Alt: alt})
				m := next.(model)
				if m.done != (!paste && !alt) || m.result != (confirm.Result{}) {
					t.Fatalf("%q paste=%t alt=%t: incorrect denial", text, paste, alt)
				}
			}
		}
	}
}

func TestAltKeysDoNotConfirm(t *testing.T) {
	for _, m := range []model{key(key(ready(), tea.KeyTab), tea.KeyTab), editing()} {
		for _, typ := range []tea.KeyType{tea.KeyCtrlS, tea.KeyCtrlC, tea.KeyEsc, tea.KeyEnter} {
			next, _ := m.Update(tea.KeyMsg{Type: typ, Alt: true})
			got := next.(model)
			if got.done || got.editing != m.editing || got.result != (confirm.Result{}) {
				t.Fatalf("alt+%s triggered a decision", typ)
			}
		}
	}
}

func TestFeedbackLimitPreservesDraft(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("a", 4096) + "DO NOT PUSH",
		strings.Repeat("日", 4096) + "末尾の指示",
		strings.Repeat("a\n", 2048) + "END",
	} {
		for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 87, Height: 16}} {
			m := editing()
			next, _ := m.Update(size)
			m = typeText(next.(model), text)
			m = key(m, tea.KeyCtrlS)
			if m.done || !m.editing || m.result != (confirm.Result{}) || m.feedback.Value() != text {
				t.Fatalf("long feedback was truncated or submitted: got %d bytes, want %d", len(m.feedback.Value()), len(text))
			}
			view := ansi.Strip(m.View())
			if !strings.Contains(view, "Shorten feedback to send") || !strings.Contains(view, "/4096 chars") {
				t.Fatalf("missing feedback limit notice:\n%s", view)
			}
			if height := strings.Count(view, "\n") + 1; height > size.Height {
				t.Fatalf("%dx%d: limit notice overflows vertically:\n%s", size.Width, size.Height, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size.Width {
					t.Fatalf("%dx%d: limit notice overflows horizontally: %q", size.Width, size.Height, line)
				}
			}
			m = key(m, tea.KeyEsc)
			m = key(key(m, tea.KeyTab), tea.KeyEnter)
			if m.feedback.Value() != text {
				t.Fatal("returning to feedback lost the oversized draft")
			}
			m = key(m, tea.KeyCtrlC)
			if !m.done || m.result != (confirm.Result{}) {
				t.Fatal("could not deny with an oversized draft")
			}
		}
	}
}

func TestFeedbackLimitCanBeCorrected(t *testing.T) {
	for _, text := range []string{strings.Repeat("a", 4096), strings.Repeat("日", 4096)} {
		m := typeText(editing(), text+"X")
		m = key(m, tea.KeyBackspace)
		if m.feedback.Value() != text || !strings.Contains(ansi.Strip(m.View()), "4096/4096 chars") {
			t.Fatal("editing did not restore the character budget")
		}
		m = key(m, tea.KeyCtrlS)
		if !m.done || m.result.Approved || m.result.Feedback != text {
			t.Fatal("feedback at the limit was not submitted intact")
		}
	}
}

func TestFeedbackBackAndPlainActions(t *testing.T) {
	for _, action := range []int{denyAction, approveAction} {
		m := typeText(editing(), "まだ送らない下書き")
		m = key(m, tea.KeyEsc)
		if m.editing || m.done || m.selected != denyAction || m.feedback.Focused() {
			t.Fatal("Escape did not return to the default selection")
		}
		m = key(key(m, tea.KeyTab), tea.KeyEnter)
		if m.feedback.Value() != "まだ送らない下書き" {
			t.Fatal("returning to feedback lost the draft")
		}
		m = key(m, tea.KeyEsc)
		for range action {
			m = key(m, tea.KeyTab)
		}
		m = key(m, tea.KeyEnter)
		if !m.done || m.result.Approved != (action == approveAction) || m.result.Feedback != "" {
			t.Fatalf("plain action sent the draft or changed decision: %+v", m.result)
		}
	}
	for _, text := range []string{"", " \n  \n", "送信せず拒否"} {
		m := typeText(editing(), text)
		m = key(m, tea.KeyCtrlC)
		if !m.done || m.result != (confirm.Result{}) {
			t.Fatalf("Ctrl+C sent the draft: %+v", m.result)
		}
	}
	m := key(typeText(editing(), " \n  \n"), tea.KeyCtrlS)
	if !m.done || m.result != (confirm.Result{}) {
		t.Fatalf("whitespace was sent as feedback: %+v", m.result)
	}
}

func TestFeedbackDeadline(t *testing.T) {
	for _, text := range []string{"期限切れの下書き", strings.Repeat("a", 4097)} {
		for _, msg := range []tea.Msg{tick(time.Now()), tea.KeyMsg{Type: tea.KeyCtrlS}} {
			m := typeText(editing(), text)
			m.request.Deadline = time.Now().Add(-time.Second)
			next, _ := m.Update(msg)
			m = next.(model)
			if !m.done || m.result != (confirm.Result{}) {
				t.Fatalf("expired feedback was sent: %+v", m.result)
			}
		}
	}
}

func TestFeedbackLayoutAndResize(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 40, Height: 14}, {Width: 87, Height: 16}, {Width: 120, Height: 40}} {
		for _, text := range []string{"", strings.Repeat("日本語の修正指示\n", 150) + "END"} {
			m := typeText(editing(), text)
			draft := m.feedback.Value()
			next, _ := m.Update(size)
			m = next.(model)
			view := ansi.Strip(m.View())
			if m.feedback.Value() != draft || !m.editing {
				t.Fatal("resize changed the draft or left the editor")
			}
			if height := strings.Count(view, "\n") + 1; height > size.Height {
				t.Fatalf("%dx%d: editor is %d lines:\n%s", size.Width, size.Height, height, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if width := ansi.StringWidth(line); width > size.Width {
					t.Fatalf("%dx%d: editor line is %d columns: %q", size.Width, size.Height, width, line)
				}
			}
			for _, label := range []string{"Deny with feedback", "Ctrl+S: send feedback and deny", "Expires in"} {
				if !strings.Contains(view, label) {
					t.Fatalf("%dx%d: missing %q:\n%s", size.Width, size.Height, label, view)
				}
			}
			if text != "" && !strings.Contains(view, "END") {
				t.Fatalf("%dx%d: cannot see the end of feedback:\n%s", size.Width, size.Height, view)
			}
		}
	}
	m := typeText(editing(), "下書き")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 35, Height: 11})
	m = key(next.(model), tea.KeyCtrlS)
	if m.done || m.result != (confirm.Result{}) {
		t.Fatal("submitted feedback with insufficient room to review")
	}
	m = key(m, tea.KeyCtrlC)
	if !m.done || m.result != (confirm.Result{}) {
		t.Fatal("could not deny in a small terminal")
	}
}

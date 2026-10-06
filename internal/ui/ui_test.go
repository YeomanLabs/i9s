package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/YeomanLabs/i9s/internal/demo"
)

func TestTableFilterSortAndCursor(t *testing.T) {
	tb := &Table{Cols: []Column{{Title: "NAME"}, {Title: "STATE"}}}
	tb.SetRows([]Row{
		{ID: "1", Cells: []string{"bravo", "ok"}},
		{ID: "2", Cells: []string{"alpha", "bad"}, Hidden: "serial-xyz"},
		{ID: "3", Cells: []string{"charlie", "bad"}},
	})
	if tb.Rows()[0].ID != "2" {
		t.Fatal("not sorted by first column")
	}
	tb.SetFilter("bad")
	if tb.Len() != 2 {
		t.Fatalf("filter: %d rows", tb.Len())
	}
	tb.SetFilter("!bad")
	if tb.Len() != 1 || tb.Rows()[0].ID != "1" {
		t.Fatal("negated filter")
	}
	tb.SetFilter("xyz")
	if tb.Len() != 1 || tb.Rows()[0].ID != "2" {
		t.Fatal("hidden text isn't searchable")
	}
	tb.SetFilter("")
	if !tb.SortHotkey('S') || tb.SortBy != 1 {
		t.Fatal("shift-S should sort by STATE")
	}
	tb.SortHotkey('S')
	if !tb.Desc {
		t.Fatal("second press should reverse")
	}
	// The cursor follows the same item across a refresh.
	tb.Cursor = 2
	id := tb.Current().ID
	tb.SetRows(append([]Row{{ID: "0", Cells: []string{"aaa", "zzz"}}}, tb.all...))
	if tb.Current().ID != id {
		t.Fatalf("cursor jumped from %s to %s", id, tb.Current().ID)
	}
}

func TestRelease(t *testing.T) {
	for in, want := range map[string]string{"10.0.26100.6899": "Win11 24H2", "10.0.19045.1": "Win10 22H2", "10.0.99999.1": "Build 99999", "": "-"} {
		if got := Release(in); got != want {
			t.Errorf("Release(%q) = %q, want %q", in, got, want)
		}
	}
}

// drive feeds messages and follows returned commands synchronously.
type drive struct{ m tea.Model }

func (d *drive) send(msg tea.Msg) {
	var cmd tea.Cmd
	d.m, cmd = d.m.Update(msg)
	d.exec(cmd)
}

func (d *drive) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				d.exec(c)
			}
			return
		}
		if tn := fmt.Sprintf("%T", msg); msg != nil && !strings.Contains(tn, "ickMsg") {
			d.send(msg)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func (d *drive) keys(s string) {
	for _, r := range s {
		d.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestEveryScreenRenders(t *testing.T) {
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	src := demo.NewAt(now)
	src.Latency = 0
	m := New(src, Options{Now: func() time.Time { return now }})
	d := &drive{m: m}
	d.exec(m.Init())
	d.send(tea.WindowSizeMsg{Width: 140, Height: 40})

	view := func() string { return d.m.View() }
	if !strings.Contains(view(), "Devices(all)[458]") {
		t.Fatalf("devices list missing:\n%s", view())
	}

	d.keys("/")
	d.keys("notcompliant")
	d.send(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(view(), "Devices(notcompliant)[42]") {
		t.Fatal("filter didn't apply")
	}
	d.send(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(view(), "Describe(") || !strings.Contains(view(), "Defender") {
		t.Fatal("describe view")
	}
	d.send(tea.KeyMsg{Type: tea.KeyEsc})

	d.keys(":")
	d.keys("users")
	d.send(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(view(), "Users(all)[458]") {
		t.Fatal("users view")
	}
	d.keys("3")
	d.send(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(view(), "InstallStatus(") {
		t.Fatal("app drill-down")
	}
	d.keys("4")
	if !strings.Contains(view(), "Windows release") {
		t.Fatal("pulse")
	}

	// Restart asks first, and "n" cancels without calling the source.
	d.keys("1")
	d.send(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !strings.Contains(view(), "Restart ") {
		t.Fatal("no confirm dialog")
	}
	d.keys("n")
	if strings.Contains(view(), "loses unsaved work") {
		t.Fatal("dialog didn't close")
	}

	d.keys(":")
	d.keys("nope")
	d.send(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(view(), "unknown command") {
		t.Fatal("bad command should flash an error")
	}
}

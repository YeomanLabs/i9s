package ui

import (
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Column describes one table column.
type Column struct {
	Title string
	// Max width; 0 means "take the leftover space".
	Max int
	// Right-align (numbers, ages).
	Right bool
	// Sort key for a row; defaults to the cell text.
	SortKey func(r Row) string
}

// Row is one line of a resource table.
type Row struct {
	ID    string
	Cells []string
	Tone  tone
	// Extra searchable text that isn't shown (serials, ids).
	Hidden string
}

// Table is a filterable, sortable, scrollable list, rendered k9s style.
type Table struct {
	Cols   []Column
	all    []Row
	rows   []Row // after filter + sort
	Cursor int
	offset int
	Filter string
	SortBy int
	Desc   bool
}

func (t *Table) SetRows(rows []Row) {
	var keep string
	if c := t.Current(); c != nil {
		keep = c.ID
	}
	t.all = rows
	t.apply()
	// Stay on the same item across refreshes.
	if keep != "" {
		for i, r := range t.rows {
			if r.ID == keep {
				t.Cursor = i
				break
			}
		}
	}
	t.clamp()
}

func (t *Table) Len() int      { return len(t.rows) }
func (t *Table) Total() int    { return len(t.all) }
func (t *Table) Rows() []Row   { return t.rows }
func (t *Table) Current() *Row {
	if t.Cursor >= 0 && t.Cursor < len(t.rows) {
		return &t.rows[t.Cursor]
	}
	return nil
}

func (t *Table) SetFilter(f string) {
	t.Filter = f
	t.Cursor, t.offset = 0, 0
	t.apply()
}

// SortOn sorts by a column; asking again flips the direction.
func (t *Table) SortOn(col int) {
	if col == t.SortBy {
		t.Desc = !t.Desc
	} else {
		t.SortBy, t.Desc = col, false
	}
	t.apply()
}

// SortHotkey maps Shift+letter to the first column starting with that letter.
func (t *Table) SortHotkey(r rune) bool {
	for i, c := range t.Cols {
		if c.Title != "" && unicode.ToUpper(rune(c.Title[0])) == r {
			t.SortOn(i)
			return true
		}
	}
	return false
}

func (t *Table) apply() {
	f := strings.ToLower(strings.TrimSpace(t.Filter))
	neg := strings.HasPrefix(f, "!")
	f = strings.TrimPrefix(f, "!")
	terms := strings.Fields(f)
	t.rows = t.rows[:0]
	for _, r := range t.all {
		hay := strings.ToLower(strings.Join(r.Cells, " ") + " " + r.Hidden)
		match := true
		for _, term := range terms {
			if !strings.Contains(hay, term) {
				match = false
				break
			}
		}
		if len(terms) == 0 || match != neg {
			t.rows = append(t.rows, r)
		}
	}
	if t.SortBy >= 0 && t.SortBy < len(t.Cols) {
		key := t.Cols[t.SortBy].SortKey
		if key == nil {
			i := t.SortBy
			key = func(r Row) string { return strings.ToLower(r.Cells[i]) }
		}
		sort.SliceStable(t.rows, func(a, b int) bool {
			ka, kb := key(t.rows[a]), key(t.rows[b])
			if t.Desc {
				return ka > kb
			}
			return ka < kb
		})
	}
	t.clamp()
}

func (t *Table) Move(n int) {
	t.Cursor += n
	t.clamp()
}

func (t *Table) Home() { t.Cursor = 0; t.clamp() }
func (t *Table) End()  { t.Cursor = len(t.rows) - 1; t.clamp() }

func (t *Table) clamp() {
	if t.Cursor >= len(t.rows) {
		t.Cursor = len(t.rows) - 1
	}
	if t.Cursor < 0 {
		t.Cursor = 0
	}
}

// widths fits columns into the available width.
func (t *Table) widths(width int) []int {
	n := len(t.Cols)
	w := make([]int, n)
	flex := -1
	used := 0
	for i, c := range t.Cols {
		need := runewidth.StringWidth(c.Title) + 2 // room for the sort arrow
		for _, r := range t.all {
			if i < len(r.Cells) {
				if cw := runewidth.StringWidth(r.Cells[i]); cw > need {
					need = cw
				}
			}
		}
		if c.Max == 0 {
			flex = i
			w[i] = need
			continue
		}
		if need > c.Max {
			need = c.Max
		}
		w[i] = need
		used += need
	}
	gaps := (n - 1) * 2
	if flex >= 0 {
		rest := width - used - gaps - 2
		if rest < 8 {
			rest = 8
		}
		w[flex] = rest
	}
	// Shrink from the right if the terminal is narrow.
	for total := sum(w) + gaps + 2; total > width; total = sum(w) + gaps + 2 {
		shrunk := false
		for i := n - 1; i >= 0 && !shrunk; i-- {
			if w[i] > 6 {
				w[i]--
				shrunk = true
			}
		}
		if !shrunk {
			break
		}
	}
	return w
}

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

func fit(s string, w int, right bool) string {
	if runewidth.StringWidth(s) > w {
		s = runewidth.Truncate(s, w, "…")
	}
	pad := w - runewidth.StringWidth(s)
	if pad < 0 {
		pad = 0
	}
	if right {
		return strings.Repeat(" ", pad) + s
	}
	return s + strings.Repeat(" ", pad)
}

// View renders the table body (header + rows) into exactly height lines.
func (t *Table) View(width, height int) string {
	if height < 2 {
		return ""
	}
	w := t.widths(width)
	var b strings.Builder

	hdr := make([]string, len(t.Cols))
	for i, c := range t.Cols {
		title := c.Title
		if i == t.SortBy {
			if t.Desc {
				title += "↓"
			} else {
				title += "↑"
			}
		}
		hdr[i] = fit(title, w[i], c.Right)
	}
	b.WriteString(sHeader.Render(" " + strings.Join(hdr, "  ") + " "))
	b.WriteByte('\n')

	visible := height - 1
	if t.Cursor < t.offset {
		t.offset = t.Cursor
	}
	if t.Cursor >= t.offset+visible {
		t.offset = t.Cursor - visible + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
	lines := 0
	for i := t.offset; i < len(t.rows) && lines < visible; i++ {
		r := t.rows[i]
		cells := make([]string, len(t.Cols))
		for j, c := range t.Cols {
			v := ""
			if j < len(r.Cells) {
				v = r.Cells[j]
			}
			cells[j] = fit(v, w[j], c.Right)
		}
		line := " " + strings.Join(cells, "  ") + " "
		if i == t.Cursor {
			b.WriteString(sSel.Render(line))
		} else {
			b.WriteString(toneStyle(r.Tone).Render(line))
		}
		b.WriteByte('\n')
		lines++
	}
	if len(t.rows) == 0 {
		b.WriteString(sMuted.Render(fit("  Nothing to show.", width, false)))
		b.WriteByte('\n')
		lines++
	}
	for ; lines < visible; lines++ {
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// boxed draws a k9s-style frame with a centered title.
func boxed(title string, body string, width, height int) string {
	inner := width - 2
	tw := lipgloss.Width(title)
	left := (inner - tw) / 2
	if left < 1 {
		left = 1
	}
	right := inner - tw - left
	if right < 0 {
		right = 0
	}
	var b strings.Builder
	b.WriteString(sBorder.Render("┌" + strings.Repeat("─", left)))
	b.WriteString(title)
	b.WriteString(sBorder.Render(strings.Repeat("─", right) + "┐"))
	b.WriteByte('\n')
	lines := strings.Split(body, "\n")
	for i := 0; i < height-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		if pad := inner - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		b.WriteString(sBorder.Render("│"))
		b.WriteString(l)
		b.WriteString(sBorder.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(sBorder.Render("└" + strings.Repeat("─", inner) + "┘"))
	return b.String()
}

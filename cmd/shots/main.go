// shots drives i9s against the demo tenant and writes each screen as a
// self-contained HTML "terminal" (docs/shots/*.html). scripts/shots.mjs turns
// them into PNGs for the README. Deterministic: fixed clock, no latency.
package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/YeomanLabs/i9s/internal/demo"
	"github.com/YeomanLabs/i9s/internal/ui"
)

const cols, rows = 150, 42

var now = time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)

type driver struct{ m tea.Model }

// run feeds a message and synchronously follows the commands it returns,
// skipping timers (ticks and spinners never finish within the wait).
func (d *driver) send(msg tea.Msg) {
	var cmd tea.Cmd
	d.m, cmd = d.m.Update(msg)
	d.exec(cmd)
}

func (d *driver) exec(cmd tea.Cmd) {
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
		// Animation ticks reschedule themselves forever; screenshots don't need them.
		if t := fmt.Sprintf("%T", msg); msg != nil && !strings.Contains(t, "tickMsg") && !strings.Contains(t, "TickMsg") {
			d.send(msg)
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func (d *driver) keys(s string) {
	for _, r := range s {
		d.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (d *driver) key(t tea.KeyType) { d.send(tea.KeyMsg{Type: t}) }

func main() {
	lipgloss.SetColorProfile(termenv.TrueColor)
	out := "docs/shots"
	_ = os.MkdirAll(out, 0o755)

	fresh := func() *driver {
		src := demo.NewAt(now)
		src.Latency = 0
		m := ui.New(src, ui.Options{Now: func() time.Time { return now }})
		ui.Version = "v0.1.0"
		d := &driver{m: m}
		d.exec(m.Init())
		d.send(tea.WindowSizeMsg{Width: cols, Height: rows})
		return d
	}
	save := func(d *driver, name string) {
		path := filepath.Join(out, name+".html")
		if err := os.WriteFile(path, []byte(toHTML(d.m.View())), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("wrote", path)
	}

	d := fresh()
	d.keys("L") // sort by last sync
	d.keys("L") // newest first? no: oldest at top makes stale ones visible
	save(d, "devices")

	d = fresh()
	d.keys("/")
	d.keys("notcompliant")
	save(d, "filter")
	d.key(tea.KeyEnter)
	d.key(tea.KeyDown)
	d.key(tea.KeyDown)
	d.key(tea.KeyEnter)
	save(d, "describe")

	d = fresh()
	d.keys(":")
	d.keys("apps")
	d.key(tea.KeyEnter)
	for i := 0; i < 5; i++ { // GlobalProtect VPN
		d.key(tea.KeyDown)
	}
	d.key(tea.KeyEnter)
	save(d, "appstatus")

	d = fresh()
	d.keys("4")
	d.exec(func() tea.Msg { return nil })
	save(d, "pulse")

	d = fresh()
	d.keys("2")
	save(d, "users")

	d = fresh()
	d.keys("?")
	save(d, "help")

	d = fresh()
	d.key(tea.KeyDown)
	d.send(tea.KeyMsg{Type: tea.KeyCtrlR})
	save(d, "restart")
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// toHTML renders ANSI truecolor output as a terminal-looking HTML page.
func toHTML(s string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><style>
html,body{margin:0;background:#0b0e1a}
.term{display:inline-block;margin:0;padding:18px 22px;background:#0b0e1a;color:#d8e1ff;font:15px/1.32 'Cascadia Mono','JetBrains Mono',Consolas,monospace;white-space:pre}
</style></head><body><pre class="term">`)
	fg, bg, bold := "", "", false
	open := false
	flush := func() {
		if open {
			b.WriteString("</span>")
			open = false
		}
		if fg == "" && bg == "" && !bold {
			return
		}
		st := ""
		if fg != "" {
			st += "color:" + fg + ";"
		}
		if bg != "" {
			st += "background:" + bg + ";"
		}
		if bold {
			st += "font-weight:700;"
		}
		b.WriteString(`<span style="` + st + `">`)
		open = true
	}
	last := 0
	for _, m := range sgr.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:m[0]]))
		last = m[1]
		params := strings.Split(s[m[2]:m[3]], ";")
		for i := 0; i < len(params); i++ {
			p, _ := strconv.Atoi(params[i])
			switch {
			case params[i] == "" || p == 0:
				fg, bg, bold = "", "", false
			case p == 1:
				bold = true
			case p == 22:
				bold = false
			case p == 39:
				fg = ""
			case p == 49:
				bg = ""
			case (p == 38 || p == 48) && i+4 < len(params) && params[i+1] == "2":
				r, _ := strconv.Atoi(params[i+2])
				g, _ := strconv.Atoi(params[i+3])
				bl, _ := strconv.Atoi(params[i+4])
				c := fmt.Sprintf("#%02x%02x%02x", r, g, bl)
				if p == 38 {
					fg = c
				} else {
					bg = c
				}
				i += 4
			}
		}
		flush()
	}
	b.WriteString(html.EscapeString(s[last:]))
	if open {
		b.WriteString("</span>")
	}
	b.WriteString("</pre></body></html>")
	return b.String()
}

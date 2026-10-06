// Package ui is the i9s terminal interface, built on Bubble Tea.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/YeomanLabs/i9s/internal/intune"
)

// Version is set by the build.
var Version = "dev"

const timeout = 90 * time.Second

type Options struct {
	Refresh time.Duration // auto-refresh for list views; 0 = off
	Start   string        // initial command, e.g. "users" or "devices noncompliant"
	Now     func() time.Time
}

type Model struct {
	src  intune.Source
	opts Options
	w, h int

	view  view
	stack []view

	devices   []intune.Device
	users     []intune.User
	apps      []intune.App
	app       intune.App
	appStatus []intune.AppDeviceStatus
	detail    *intune.DeviceDetail

	tables  map[view]*Table
	loaded  map[view]time.Time
	loading map[view]bool
	errs    map[view]error

	vp      viewport.Model
	spin    spinner.Model
	input   textinput.Model
	prompt  rune // 0, ':' or '/'
	help    bool
	confirm *confirm

	flash    string
	flashErr bool
	flashAt  time.Time
}

type confirm struct {
	title, body string
	yes         bool
	run         tea.Cmd
}

// ---------------------------------------------------------------- messages

type devicesMsg struct {
	v   []intune.Device
	err error
}
type usersMsg struct {
	v   []intune.User
	err error
}
type appsMsg struct {
	v   []intune.App
	err error
}
type appStatusMsg struct {
	app intune.App
	v   []intune.AppDeviceStatus
	err error
}
type detailMsg struct {
	v   intune.DeviceDetail
	err error
}
type actionMsg struct {
	what, device string
	err          error
}
type tickMsg time.Time

func New(src intune.Source, o Options) *Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 120
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(cAccent)
	m := &Model{
		src: src, opts: o, view: vDevices, input: ti, spin: sp,
		loaded: map[view]time.Time{}, loading: map[view]bool{}, errs: map[view]error{},
		tables: map[view]*Table{
			vDevices:   {Cols: deviceColumns(), SortBy: 0},
			vUsers:     {Cols: userColumns(), SortBy: 0},
			vApps:      {Cols: appColumns(), SortBy: 0},
			vAppStatus: {Cols: appStatusColumns(), SortBy: 2},
		},
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.tick()}
	if m.opts.Start != "" {
		if c := m.runCommand(m.opts.Start); c != nil {
			cmds = append(cmds, c)
		}
	} else {
		cmds = append(cmds, m.load(vDevices))
	}
	return tea.Batch(cmds...)
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---------------------------------------------------------------- loading

func (m *Model) load(v view) tea.Cmd {
	if m.loading[v] {
		return nil
	}
	m.loading[v] = true
	src := m.src
	switch v {
	case vDevices:
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			d, err := src.Devices(ctx)
			return devicesMsg{d, err}
		}
	case vUsers:
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			u, err := src.Users(ctx)
			return usersMsg{u, err}
		}
	case vApps:
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			a, err := src.Apps(ctx)
			return appsMsg{a, err}
		}
	case vAppStatus:
		app := m.app
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			s, err := src.AppStatus(ctx, app.ID)
			return appStatusMsg{app, s, err}
		}
	case vPulse:
		// Pulse is built from devices and users.
		m.loading[v] = false
		return tea.Batch(m.loadIfStale(vDevices), m.loadIfStale(vUsers))
	}
	m.loading[v] = false
	return nil
}

func (m *Model) loadIfStale(v view) tea.Cmd {
	if _, ok := m.loaded[v]; ok {
		return nil
	}
	return m.load(v)
}

func (m *Model) loadDetail(id string) tea.Cmd {
	m.loading[vDescribe] = true
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		d, err := src.DeviceDetail(ctx, id)
		return detailMsg{d, err}
	}
}

// ---------------------------------------------------------------- navigation

func (m *Model) push(v view) {
	if m.view != v {
		m.stack = append(m.stack, m.view)
	}
	m.view = v
}

func (m *Model) back() {
	if n := len(m.stack); n > 0 {
		m.view = m.stack[n-1]
		m.stack = m.stack[:n-1]
	}
}

// goTo switches to a top-level resource (clears the back stack, like k9s).
func (m *Model) goTo(v view) tea.Cmd {
	m.stack = nil
	m.view = v
	if v == vPulse {
		return m.load(vPulse)
	}
	return m.loadIfStale(v)
}

func (m *Model) runCommand(line string) tea.Cmd {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return nil
	}
	name := strings.ToLower(fields[0])
	switch name {
	case "q", "q!", "quit", "exit":
		return tea.Quit
	case "help", "?":
		m.help = true
		return nil
	}
	v, ok := commands[name]
	if !ok {
		m.setFlash(fmt.Sprintf("unknown command %q. Try :devices, :users, :apps or :pulse", name), true)
		return nil
	}
	cmd := m.goTo(v)
	if t := m.tables[v]; t != nil {
		t.SetFilter(strings.Join(fields[1:], " "))
	}
	return cmd
}

func (m *Model) setFlash(s string, isErr bool) {
	m.flash, m.flashErr, m.flashAt = s, isErr, m.opts.Now()
}

// ---------------------------------------------------------------- actions

func (m *Model) selectedDevice() (id, name string, ok bool) {
	switch m.view {
	case vDevices:
		if r := m.tables[vDevices].Current(); r != nil {
			return r.ID, r.Cells[0], true
		}
	case vAppStatus:
		if r := m.tables[vAppStatus].Current(); r != nil {
			return r.ID, r.Cells[0], true
		}
	case vDescribe:
		if m.detail != nil {
			return m.detail.ID, m.detail.Name, true
		}
	}
	return "", "", false
}

func (m *Model) action(what string) tea.Cmd {
	id, name, ok := m.selectedDevice()
	if !ok {
		return nil
	}
	src := m.src
	run := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var err error
		if what == "restart" {
			err = src.Restart(ctx, id)
		} else {
			err = src.Sync(ctx, id)
		}
		return actionMsg{what, name, err}
	}
	if what == "restart" {
		m.confirm = &confirm{
			title: "Restart " + name + "?",
			body:  "Intune tells the device to restart. Whoever is using it gets a short warning and then loses unsaved work.",
			run:   run,
		}
		return nil
	}
	m.setFlash("Sync requested for "+name+"…", false)
	return run
}

// ---------------------------------------------------------------- update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.vp.Width, m.vp.Height = m.w-4, m.bodyHeight()-2
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, m.tick())
		if m.flash != "" && m.opts.Now().Sub(m.flashAt) > 6*time.Second {
			m.flash = ""
		}
		if r := m.opts.Refresh; r > 0 {
			for _, v := range []view{vDevices, vUsers, vApps} {
				if (m.view == v || (m.view == vPulse && v != vApps)) && !m.loaded[v].IsZero() && m.opts.Now().Sub(m.loaded[v]) > r {
					cmds = append(cmds, m.load(v))
				}
			}
		}
		return m, tea.Batch(cmds...)

	case devicesMsg:
		m.loading[vDevices] = false
		m.errs[vDevices] = msg.err
		if msg.err == nil {
			m.devices = msg.v
			m.loaded[vDevices] = m.opts.Now()
			m.tables[vDevices].SetRows(deviceRows(m.devices, m.opts.Now()))
		}
		return m, nil

	case usersMsg:
		m.loading[vUsers] = false
		m.errs[vUsers] = msg.err
		if msg.err == nil {
			m.users = msg.v
			m.loaded[vUsers] = m.opts.Now()
			m.tables[vUsers].SetRows(userRows(m.users, m.opts.Now()))
		}
		return m, nil

	case appsMsg:
		m.loading[vApps] = false
		m.errs[vApps] = msg.err
		if msg.err == nil {
			m.apps = msg.v
			m.loaded[vApps] = m.opts.Now()
			m.tables[vApps].SetRows(appRows(m.apps))
		}
		return m, nil

	case appStatusMsg:
		m.loading[vAppStatus] = false
		m.errs[vAppStatus] = msg.err
		if msg.err == nil && msg.app.ID == m.app.ID {
			m.appStatus = msg.v
			m.loaded[vAppStatus] = m.opts.Now()
			m.tables[vAppStatus].SetRows(appStatusRows(m.appStatus))
		}
		return m, nil

	case detailMsg:
		m.loading[vDescribe] = false
		m.errs[vDescribe] = msg.err
		if msg.err == nil {
			d := msg.v
			m.detail = &d
			m.vp.SetContent(describe(d, m.opts.Now()))
			m.vp.GotoTop()
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlash(fmt.Sprintf("%s failed for %s: %v", msg.what, msg.device, msg.err), true)
			return m, nil
		}
		if msg.what == "restart" {
			m.setFlash("Restart sent to "+msg.device+". It happens next time the device checks in.", false)
		} else {
			m.setFlash("Sync sent to "+msg.device+". Results show after its next check-in.", false)
		}
		// Refresh so a demo sync shows up straight away.
		return m, m.load(vDevices)

	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) key(k tea.KeyMsg) tea.Cmd {
	s := k.String()
	if s == "ctrl+c" {
		return tea.Quit
	}

	// Modal: confirm dialog.
	if c := m.confirm; c != nil {
		switch s {
		case "left", "right", "tab", "h", "l":
			c.yes = !c.yes
		case "y":
			m.confirm = nil
			return c.run
		case "enter":
			m.confirm = nil
			if c.yes {
				return c.run
			}
		case "n", "esc", "q":
			m.confirm = nil
		}
		return nil
	}

	if m.help {
		if s == "?" || s == "esc" || s == "q" {
			m.help = false
		}
		return nil
	}

	// Prompt: ":" command or "/" filter.
	if m.prompt != 0 {
		switch s {
		case "esc":
			if m.prompt == '/' {
				m.table().SetFilter("")
			}
			m.prompt = 0
			m.input.Blur()
			return nil
		case "enter":
			p, val := m.prompt, m.input.Value()
			m.prompt = 0
			m.input.Blur()
			if p == ':' {
				return m.runCommand(val)
			}
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(k)
		if m.prompt == '/' {
			if t := m.table(); t != nil {
				t.SetFilter(m.input.Value())
			}
		}
		return cmd
	}

	switch s {
	case ":":
		m.prompt = ':'
		m.input.SetValue("")
		m.input.Placeholder = "devices · users · apps · pulse · quit"
		return m.input.Focus()
	case "/":
		if m.table() == nil {
			return nil
		}
		m.prompt = '/'
		m.input.SetValue(m.table().Filter)
		m.input.Placeholder = "filter (prefix ! to exclude)"
		return m.input.Focus()
	case "?":
		m.help = true
		return nil
	case "q":
		if m.view == vDescribe || m.view == vAppStatus {
			m.back()
			return nil
		}
		return tea.Quit
	case "esc":
		if t := m.table(); t != nil && t.Filter != "" {
			t.SetFilter("")
			return nil
		}
		m.back()
		return nil
	case "ctrl+r":
		if m.view == vDevices || m.view == vAppStatus || m.view == vDescribe {
			return m.action("restart")
		}
	case "s":
		if m.view == vDevices || m.view == vAppStatus || m.view == vDescribe {
			return m.action("sync")
		}
	case "r", "ctrl+l":
		switch m.view {
		case vDescribe:
			if m.detail != nil {
				return m.loadDetail(m.detail.ID)
			}
		case vPulse:
			return tea.Batch(m.load(vDevices), m.load(vUsers))
		default:
			return m.load(m.view)
		}
	case "1":
		return m.goTo(vDevices)
	case "2":
		return m.goTo(vUsers)
	case "3":
		return m.goTo(vApps)
	case "4":
		return m.goTo(vPulse)
	}

	if m.view == vDescribe {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(k)
		return cmd
	}

	t := m.table()
	if t == nil {
		return nil
	}
	switch s {
	case "up", "k":
		t.Move(-1)
	case "down", "j":
		t.Move(1)
	case "pgup", "ctrl+b":
		t.Move(-(m.bodyHeight() - 3))
	case "pgdown", "ctrl+f", " ":
		t.Move(m.bodyHeight() - 3)
	case "home", "g":
		t.Home()
	case "end", "G":
		t.End()
	case "enter", "d":
		return m.enter()
	default:
		// Shift+letter sorts by the column with that initial, like k9s.
		if r := []rune(s); len(r) == 1 && r[0] >= 'A' && r[0] <= 'Z' {
			t.SortHotkey(r[0])
		}
	}
	return nil
}

func (m *Model) enter() tea.Cmd {
	t := m.table()
	r := t.Current()
	if r == nil {
		return nil
	}
	switch m.view {
	case vDevices, vAppStatus:
		m.detail = nil
		m.push(vDescribe)
		return m.loadDetail(r.ID)
	case vApps:
		for _, a := range m.apps {
			if a.ID == r.ID {
				m.app = a
			}
		}
		m.appStatus = nil
		m.tables[vAppStatus].SetRows(nil)
		m.tables[vAppStatus].SetFilter("")
		delete(m.loaded, vAppStatus)
		m.push(vAppStatus)
		return m.load(vAppStatus)
	case vUsers:
		// Jump to this person's devices.
		upn := r.Cells[0]
		m.push(vDevices)
		m.tables[vDevices].SetFilter(upn)
		return m.loadIfStale(vDevices)
	}
	return nil
}

func (m *Model) table() *Table {
	return m.tables[m.view]
}

func (m *Model) bodyHeight() int {
	h := m.h - headerHeight - 2 // crumbs + flash
	if m.prompt != 0 {
		h -= 3
	}
	if h < 5 {
		h = 5
	}
	return h
}

// ---------------------------------------------------------------- view

const headerHeight = 6

func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')
	if m.prompt != 0 {
		b.WriteString(m.promptBox())
		b.WriteByte('\n')
	}
	body := m.body()
	if m.confirm != nil {
		body = overlay(body, m.confirmBox(), m.w)
	} else if m.help {
		body = overlay(body, m.helpBox(), m.w)
	}
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(m.footer())
	return b.String()
}

var logo = []string{
	` _  ___      `,
	`(_)/ _ \ ___ `,
	`| | (_) / __|`,
	`| |\__, \__ \`,
	`|_|  /_/|___/`,
}

func (m *Model) header() string {
	now := m.opts.Now()
	refreshed := "-"
	if t, ok := m.loaded[m.dataView()]; ok {
		refreshed = ago(t, now) + " ago"
		if ago(t, now) == "now" {
			refreshed = "just now"
		}
	}
	tenant := m.src.Tenant()
	if m.src.Demo() {
		tenant += " " + sDemo.Render("DEMO")
	}
	info := []string{
		sInfoKey.Render("Tenant:   ") + sInfoVal.Render(tenant),
		sInfoKey.Render("User:     ") + sInfoVal.Render(m.src.Account()),
		sInfoKey.Render("Devices:  ") + sInfoVal.Render(m.countText()),
		sInfoKey.Render("Refresh:  ") + sInfoVal.Render(refreshed),
		sInfoKey.Render("i9s Rev:  ") + sInfoVal.Render(Version),
	}
	hints := m.hints()
	cols := [][]string{info}
	// Hints in columns of five, like k9s.
	for i := 0; i < len(hints); i += 5 {
		end := i + 5
		if end > len(hints) {
			end = len(hints)
		}
		cols = append(cols, hints[i:end])
	}

	var lines []string
	for row := 0; row < 5; row++ {
		var parts []string
		for ci, c := range cols {
			cell := ""
			if row < len(c) {
				cell = c[row]
			}
			wd := 34
			if ci == 0 {
				wd = 52
			}
			if ci > 0 {
				wd = 24
			}
			parts = append(parts, lipgloss.NewStyle().Width(wd).MaxWidth(wd).Render(cell))
		}
		line := strings.Join(parts, "")
		if m.w >= 110 {
			pad := m.w - lipgloss.Width(line) - lipgloss.Width(logo[row]) - 1
			if pad > 0 {
				line += strings.Repeat(" ", pad) + sLogo.Render(logo[row])
			}
		}
		lines = append(lines, lipgloss.NewStyle().MaxWidth(m.w).Render(line))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func (m *Model) countText() string {
	if len(m.devices) == 0 {
		if m.loading[vDevices] {
			return "loading…"
		}
		return "-"
	}
	bad, stale := 0, 0
	now := m.opts.Now()
	for _, d := range m.devices {
		if d.Compliance == "noncompliant" {
			bad++
		}
		if !d.LastSync.IsZero() && now.Sub(d.LastSync) > 14*24*time.Hour {
			stale++
		}
	}
	return fmt.Sprintf("%d · %d not compliant · %d stale", len(m.devices), bad, stale)
}

func hint(key, text string) string {
	return sHintKey.Render("<"+key+">") + " " + sHintText.Render(text)
}

func (m *Model) hints() []string {
	base := []string{hint(":", "command"), hint("/", "filter"), hint("?", "help"), hint("r", "refresh")}
	switch m.view {
	case vDevices:
		return append([]string{hint("enter", "describe"), hint("s", "sync"), hint("ctrl-r", "restart"), hint("shift-x", "sort")}, base...)
	case vAppStatus:
		return append([]string{hint("enter", "describe"), hint("s", "sync device"), hint("esc", "back"), hint("shift-x", "sort")}, base...)
	case vUsers:
		return append([]string{hint("enter", "their devices"), hint("shift-x", "sort")}, base...)
	case vApps:
		return append([]string{hint("enter", "install status"), hint("shift-x", "sort")}, base...)
	case vDescribe:
		return []string{hint("esc", "back"), hint("s", "sync"), hint("ctrl-r", "restart"), hint("↑↓", "scroll"), hint("r", "refresh"), hint("?", "help")}
	case vPulse:
		return []string{hint("1", "devices"), hint("2", "users"), hint("3", "apps"), hint("r", "refresh"), hint(":", "command"), hint("?", "help")}
	}
	return base
}

// dataView is the list whose freshness the header shows.
func (m *Model) dataView() view {
	switch m.view {
	case vDescribe, vPulse:
		return vDevices
	}
	return m.view
}

func (m *Model) promptBox() string {
	label := sPrompt.Render(string(m.prompt) + " ")
	body := " " + label + m.input.View()
	return boxed("", body, m.w, 3)
}

func (m *Model) body() string {
	h := m.bodyHeight()
	switch m.view {
	case vDescribe:
		title := sTitle.Render(" Describe ")
		if m.detail != nil {
			title = sTitle.Render(" Describe(") + sTitleHi.Render(m.detail.Name) + sTitle.Render(") ")
		}
		content := m.vp.View()
		if m.detail == nil {
			content = m.statusLine(vDescribe, "Loading device…")
		}
		return boxed(title, content, m.w, h)
	case vPulse:
		return boxed(sTitle.Render(" Pulse ")+m.spinIf(m.loading[vDevices] || m.loading[vUsers]), pulse(m.devices, m.users, m.opts.Now(), m.w-2, h-2), m.w, h)
	}

	t := m.table()
	name := map[view]string{vDevices: "Devices", vUsers: "Users", vApps: "Apps", vAppStatus: "InstallStatus"}[m.view]
	scope := "all"
	if m.view == vAppStatus {
		scope = m.app.Name
	}
	if t.Filter != "" {
		scope = t.Filter
	}
	title := sTitle.Render(" "+name+"(") + sTitleHi.Render(scope) + sTitle.Render(fmt.Sprintf(")[%d] ", t.Len())) + m.spinIf(m.loading[m.view])

	var content string
	if err := m.errs[m.view]; err != nil && t.Total() == 0 {
		content = "\n " + sFlashErr.Render("Couldn't load: "+err.Error())
	} else if t.Total() == 0 && m.loading[m.view] {
		content = "\n " + m.spin.View() + sMuted.Render(" Loading from Microsoft Graph…")
	} else {
		content = t.View(m.w-2, h-2)
	}
	return boxed(title, content, m.w, h)
}

func (m *Model) spinIf(on bool) string {
	if on {
		return m.spin.View() + " "
	}
	return ""
}

func (m *Model) statusLine(v view, loading string) string {
	if err := m.errs[v]; err != nil {
		return "\n " + sFlashErr.Render("Couldn't load: "+err.Error())
	}
	return "\n " + m.spin.View() + sMuted.Render(" "+loading)
}

func (m *Model) footer() string {
	crumbs := make([]string, 0, len(m.stack)+1)
	for _, v := range m.stack {
		crumbs = append(crumbs, sCrumbOff.Render(viewNames[v]))
	}
	crumbs = append(crumbs, sCrumb.Render(viewNames[m.view]))
	line := strings.Join(crumbs, " ")
	if m.flash != "" {
		st := sFlash
		if m.flashErr {
			st = sFlashErr
		}
		line += "  " + st.Render(m.flash)
	}
	return lipgloss.NewStyle().MaxWidth(m.w).Render(line)
}

func (m *Model) confirmBox() string {
	c := m.confirm
	yes, no := sCrumbOff.Render("Restart"), sCrumbOff.Render("Cancel")
	if c.yes {
		yes = lipgloss.NewStyle().Foreground(lipgloss.Color("#000")).Background(cBad).Bold(true).Padding(0, 1).Render("Restart")
	} else {
		no = sCrumb.Render("Cancel")
	}
	body := lipgloss.NewStyle().Width(54).Render(sTitleHi.Render(c.title) + "\n\n" + sHintText.Render(c.body) + "\n\n" + yes + "  " + no + "   " + sMuted.Render("y/n"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBad).Padding(1, 2).Render(body)
}

func (m *Model) helpBox() string {
	type kv struct{ k, v string }
	sections := []struct {
		title string
		keys  []kv
	}{
		{"Resources", []kv{{":devices  :dv", "Windows devices"}, {":users  :us", "People"}, {":apps  :ap", "Assigned apps"}, {":pulse  :pu", "Fleet dashboard"}, {"1 2 3 4", "Jump to a resource"}, {":dv <text>", "Open with a filter"}}},
		{"Navigation", []kv{{"↑↓  j k", "Move"}, {"pgup pgdn", "Page"}, {"g  G", "Top / bottom"}, {"enter  d", "Describe / drill in"}, {"esc", "Back / clear filter"}, {"/", "Filter (! to exclude)"}, {"Shift+letter", "Sort by that column"}}},
		{"Devices", []kv{{"s", "Sync (remote action)"}, {"ctrl+r", "Restart (asks first)"}, {"r", "Refresh"}, {":q  ctrl+c", "Quit"}}},
	}
	var cols []string
	for _, s := range sections {
		var b strings.Builder
		b.WriteString(sTitleHi.Render(s.title) + "\n")
		for _, k := range s.keys {
			b.WriteString(sHintKey.Render(fmt.Sprintf("%-14s", k.k)) + " " + sHintText.Render(k.v) + "\n")
		}
		cols = append(cols, lipgloss.NewStyle().Width(36).Render(b.String()))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Padding(1, 2).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n" + sMuted.Render("i9s is read-only except Sync and Restart. Press ? or esc to close."))
}

// overlay centers box on top of base.
func overlay(base, box string, width int) string {
	bl := strings.Split(base, "\n")
	ol := strings.Split(box, "\n")
	top := (len(bl) - len(ol)) / 2
	if top < 0 {
		top = 0
	}
	bw := lipgloss.Width(ol[0])
	left := (width - bw) / 2
	if left < 0 {
		left = 0
	}
	for i, l := range ol {
		y := top + i
		if y >= len(bl) {
			break
		}
		// Keep what's left and right of the box, so it floats over the table.
		under := bl[y]
		pre := ansi.Truncate(under, left, "")
		if pad := left - ansi.StringWidth(pre); pad > 0 {
			pre += strings.Repeat(" ", pad)
		}
		bl[y] = pre + l + ansi.TruncateLeft(under, left+bw, "")
	}
	return strings.Join(bl, "\n")
}

package ui

import "github.com/charmbracelet/lipgloss"

// A k9s-flavoured palette: cyan chrome, orange accents, status colors that
// read at a glance on dark terminals.
var (
	cAccent  = lipgloss.Color("#ff9f43")
	cCyan    = lipgloss.Color("#4fd6ff")
	cBlue    = lipgloss.Color("#6ea8fe")
	cText    = lipgloss.Color("#d8e1ff")
	cMuted   = lipgloss.Color("#7c86a8")
	cFaint   = lipgloss.Color("#4b5476")
	cGood    = lipgloss.Color("#5af78e")
	cWarn    = lipgloss.Color("#ffd75f")
	cBad     = lipgloss.Color("#ff5f87")
	cStale   = lipgloss.Color("#b48cff")
	cSelBg   = lipgloss.Color("#1f6feb")
	cSelText = lipgloss.Color("#ffffff")
	cBorder  = lipgloss.Color("#2f8fd8")

	sLogo     = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sInfoKey  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sInfoVal  = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sHintKey  = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	sHintText = lipgloss.NewStyle().Foreground(cMuted)
	sTitle    = lipgloss.NewStyle().Foreground(cCyan).Bold(true)
	sTitleHi  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sBorder   = lipgloss.NewStyle().Foreground(cBorder)
	sHeader   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sRow      = lipgloss.NewStyle().Foreground(cCyan)
	sSel      = lipgloss.NewStyle().Foreground(cSelText).Background(cSelBg).Bold(true)
	sCrumb    = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(cAccent).Bold(true).Padding(0, 1)
	sCrumbOff = lipgloss.NewStyle().Foreground(cText).Background(lipgloss.Color("#30364d")).Padding(0, 1)
	sFlash    = lipgloss.NewStyle().Foreground(cGood)
	sFlashErr = lipgloss.NewStyle().Foreground(cBad)
	sMuted    = lipgloss.NewStyle().Foreground(cMuted)
	sPrompt   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sDemo     = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(cWarn).Bold(true).Padding(0, 1)
)

// Row tones: the whole row takes the color, like k9s does for pod status.
type tone int

const (
	toneNormal tone = iota
	toneGood
	toneWarn
	toneBad
	toneStale
	toneMuted
)

func toneStyle(t tone) lipgloss.Style {
	switch t {
	case toneGood:
		return lipgloss.NewStyle().Foreground(cGood)
	case toneWarn:
		return lipgloss.NewStyle().Foreground(cWarn)
	case toneBad:
		return lipgloss.NewStyle().Foreground(cBad)
	case toneStale:
		return lipgloss.NewStyle().Foreground(cStale)
	case toneMuted:
		return lipgloss.NewStyle().Foreground(cMuted)
	}
	return sRow
}

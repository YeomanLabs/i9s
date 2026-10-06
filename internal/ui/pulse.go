package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/YeomanLabs/i9s/internal/intune"
)

type bucket struct {
	label string
	n     int
	color lipgloss.Color
}

// pulse is the at-a-glance dashboard: four panels of horizontal bars.
func pulse(ds []intune.Device, us []intune.User, now time.Time, width, height int) string {
	if len(ds) == 0 && len(us) == 0 {
		return "\n " + sMuted.Render("Loading…")
	}

	comp := map[string]int{}
	rel := map[string]int{}
	sync := []bucket{{"under 24h", 0, cGood}, {"1-7 days", 0, cCyan}, {"7-14 days", 0, cWarn}, {"14-30 days", 0, cStale}, {"30+ days", 0, cBad}}
	for _, d := range ds {
		comp[compliance(d.Compliance)]++
		rel[Release(d.OSVersion)]++
		age := now.Sub(d.LastSync)
		switch {
		case age < 24*time.Hour:
			sync[0].n++
		case age < 7*24*time.Hour:
			sync[1].n++
		case age < 14*24*time.Hour:
			sync[2].n++
		case age < 30*24*time.Hour:
			sync[3].n++
		default:
			sync[4].n++
		}
	}
	compB := []bucket{
		{"Compliant", comp["Compliant"], cGood},
		{"In grace", comp["InGrace"], cWarn},
		{"Not compliant", comp["NotCompliant"], cBad},
		{"Unknown", comp["Unknown"], cMuted},
	}
	var relB []bucket
	for k, n := range rel {
		c := cCyan
		if strings.HasPrefix(k, "Win10") {
			c = cBad
		}
		relB = append(relB, bucket{k, n, c})
	}
	sort.Slice(relB, func(i, j int) bool { return relB[i].label > relB[j].label })

	mfa := map[string]int{}
	for _, u := range us {
		if u.Enabled {
			mfa[u.MFA]++
		}
	}
	mfaB := []bucket{
		{"Passwordless", mfa["passwordless"], cGood},
		{"Authenticator", mfa["strong"], cCyan},
		{"SMS/voice only", mfa["weak"], cWarn},
		{"No MFA", mfa["none"], cBad},
	}
	if mfa[""] > 0 {
		mfaB = append(mfaB, bucket{"Unknown", mfa[""], cMuted})
	}

	colW := (width - 3) / 2
	panel := func(title string, bs []bucket, total int) string {
		var b strings.Builder
		b.WriteString(sSection.Render(title) + sMuted.Render(fmt.Sprintf("  (%d)", total)) + "\n\n")
		barW := colW - 28
		if barW < 6 {
			barW = 6
		}
		for _, x := range bs {
			frac := 0.0
			if total > 0 {
				frac = float64(x.n) / float64(total)
			}
			n := int(frac*float64(barW) + 0.5)
			if x.n > 0 && n == 0 {
				n = 1
			}
			b.WriteString(fmt.Sprintf(" %-14s ", x.label))
			b.WriteString(lipgloss.NewStyle().Foreground(x.color).Render(strings.Repeat("■", n)))
			b.WriteString(sMuted.Render(strings.Repeat("·", barW-n)))
			b.WriteString(sVal.Render(fmt.Sprintf(" %5d", x.n)) + sMuted.Render(fmt.Sprintf(" %3.0f%%", frac*100)) + "\n")
		}
		return lipgloss.NewStyle().Width(colW).Render(b.String())
	}

	users := 0
	for _, n := range mfa {
		users += n
	}
	top := lipgloss.JoinHorizontal(lipgloss.Top, " "+panel("Compliance", compB, len(ds)), "  ", panel("Last check-in", sync, len(ds)))
	bottom := lipgloss.JoinHorizontal(lipgloss.Top, " "+panel("Windows release", relB, len(ds)), "  ", panel("MFA (enabled users)", mfaB, users))
	return "\n" + top + "\n\n" + bottom
}

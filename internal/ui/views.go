package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YeomanLabs/i9s/internal/intune"
)

// view is one screen. List views keep their own Table.
type view int

const (
	vDevices view = iota
	vUsers
	vApps
	vAppStatus
	vDescribe
	vPulse
)

var viewNames = map[view]string{
	vDevices: "devices", vUsers: "users", vApps: "apps", vAppStatus: "appstatus", vDescribe: "describe", vPulse: "pulse",
}

// Commands accepted at the ":" prompt, k9s style (full names and short aliases).
var commands = map[string]view{
	"devices": vDevices, "device": vDevices, "dev": vDevices, "dv": vDevices, "do": vDevices,
	"users": vUsers, "user": vUsers, "us": vUsers, "people": vUsers,
	"apps": vApps, "app": vApps, "ap": vApps,
	"pulse": vPulse, "pulses": vPulse, "pu": vPulse,
}

// ---------------------------------------------------------------- helpers

func ago(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// sortable age key: older = larger, zero time sorts last.
func ageKey(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "z"
	}
	return fmt.Sprintf("%015d", int64(now.Sub(t)/time.Second))
}

var releases = map[string]string{
	"19044": "Win10 21H2", "19045": "Win10 22H2", "22621": "Win11 22H2", "22631": "Win11 23H2",
	"26100": "Win11 24H2", "26200": "Win11 25H2", "26300": "Win11 Insider", "28000": "Win11 26H1",
}

// Release turns "10.0.26100.6899" into "Win11 24H2".
func Release(os string) string {
	parts := strings.Split(os, ".")
	if len(parts) >= 3 {
		if r, ok := releases[parts[2]]; ok {
			return r
		}
		return "Build " + parts[2]
	}
	if os == "" {
		return "-"
	}
	return os
}

func compliance(s string) string {
	switch s {
	case "compliant":
		return "Compliant"
	case "noncompliant":
		return "NotCompliant"
	case "inGracePeriod":
		return "InGrace"
	case "configManager":
		return "ConfigMgr"
	case "":
		return "Unknown"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ---------------------------------------------------------------- devices

func deviceColumns() []Column {
	return []Column{
		{Title: "NAME", Max: 18},
		{Title: "USER", Max: 30},
		{Title: "OS", Max: 14},
		{Title: "COMPLIANCE", Max: 12},
		{Title: "LAST SYNC", Max: 11, Right: true, SortKey: func(r Row) string { return r.Hidden[:15] }},
		{Title: "MODEL", Max: 22},
		{Title: "SITE", Max: 0},
		{Title: "BITLOCKER", Max: 9},
	}
}

func deviceRows(ds []intune.Device, now time.Time) []Row {
	rows := make([]Row, len(ds))
	for i, d := range ds {
		t := toneNormal
		stale := !d.LastSync.IsZero() && now.Sub(d.LastSync) > 14*24*time.Hour
		switch {
		case d.Compliance == "noncompliant" || d.Compliance == "error" || d.Compliance == "conflict":
			t = toneBad
		case d.Compliance == "inGracePeriod":
			t = toneWarn
		case stale:
			t = toneStale
		case d.Compliance == "unknown" || d.Compliance == "":
			t = toneMuted
		}
		site := d.Category
		if site == "" || site == "Unknown" {
			site = "-"
		}
		rows[i] = Row{
			ID:     d.ID,
			Cells:  []string{d.Name, orDash(d.User), Release(d.OSVersion), compliance(d.Compliance), ago(d.LastSync, now), d.Model, site, yesNo(d.Encrypted)},
			Tone:   t,
			Hidden: ageKey(d.LastSync, now) + " " + d.Serial + " " + d.ID + " " + d.OSVersion,
		}
	}
	return rows
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---------------------------------------------------------------- users

func userColumns() []Column {
	return []Column{
		{Title: "UPN", Max: 34},
		{Title: "NAME", Max: 22},
		{Title: "DEPARTMENT", Max: 16},
		{Title: "OFFICE", Max: 14},
		{Title: "ENABLED", Max: 7},
		{Title: "LAST SIGN-IN", Max: 14, Right: true, SortKey: func(r Row) string { return r.Hidden[:15] }},
		{Title: "MFA", Max: 12},
		{Title: "LICENSES", Max: 0},
	}
}

func userRows(us []intune.User, now time.Time) []Row {
	rows := make([]Row, len(us))
	for i, u := range us {
		t := toneNormal
		switch {
		case !u.Enabled:
			t = toneMuted
		case u.MFA == "none":
			t = toneBad
		case u.MFA == "weak":
			t = toneWarn
		case !u.LastSignIn.IsZero() && now.Sub(u.LastSignIn) > 30*24*time.Hour:
			t = toneStale
		}
		mfa := u.MFA
		if mfa == "" {
			mfa = "-"
		}
		last := ago(u.LastSignIn, now)
		if u.LastSignIn.IsZero() {
			last = "-"
		}
		lic := strings.Join(u.Licenses, ", ")
		if lic == "" {
			lic = "-"
		}
		rows[i] = Row{
			ID:     u.ID,
			Cells:  []string{u.UPN, orDash(u.Name), orDash(u.Department), orDash(u.Office), yesNo(u.Enabled), last, mfa, lic},
			Tone:   t,
			Hidden: ageKey(u.LastSignIn, now) + " " + u.ID,
		}
	}
	return rows
}

// ---------------------------------------------------------------- apps

func appColumns() []Column {
	return []Column{
		{Title: "NAME", Max: 0},
		{Title: "TYPE", Max: 7},
		{Title: "PUBLISHER", Max: 22},
		{Title: "VERSION", Max: 14},
	}
}

func appRows(as []intune.App) []Row {
	rows := make([]Row, len(as))
	for i, a := range as {
		rows[i] = Row{ID: a.ID, Cells: []string{a.Name, a.Type, orDash(a.Publisher), orDash(a.Version)}, Hidden: a.ID}
	}
	return rows
}

func appStatusColumns() []Column {
	return []Column{
		{Title: "DEVICE", Max: 18},
		{Title: "USER", Max: 30},
		// Worst first: failures are what you came to see.
		{Title: "STATE", Max: 13, SortKey: func(r Row) string { return stateRank[r.Cells[2]] + r.Cells[0] }},
		{Title: "ERROR", Max: 10},
		{Title: "DETAIL", Max: 0},
	}
}

var stateRank = map[string]string{"failed": "0", "pending": "1", "notInstalled": "2", "unknown": "3", "notApplicable": "4", "installed": "5"}

func appStatusRows(ss []intune.AppDeviceStatus) []Row {
	rows := make([]Row, len(ss))
	for i, s := range ss {
		t := toneNormal
		switch s.State {
		case "failed":
			t = toneBad
		case "pending":
			t = toneWarn
		case "notApplicable", "unknown":
			t = toneMuted
		case "installed":
			t = toneGood
		}
		rows[i] = Row{ID: s.DeviceID, Cells: []string{s.DeviceName, orDash(s.User), s.State, orDash(s.ErrorCode), orDash(s.Detail)}, Tone: t, Hidden: s.DeviceID}
	}
	return rows
}

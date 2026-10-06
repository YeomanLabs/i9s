package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/YeomanLabs/i9s/internal/intune"
)

var (
	sKey     = lipgloss.NewStyle().Foreground(cBlue)
	sVal     = lipgloss.NewStyle().Foreground(cText)
	sSection = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
)

// describe renders a device like `kubectl describe`: aligned key/value
// sections, with problems colored.
func describe(d intune.DeviceDetail, now time.Time) string {
	var b strings.Builder
	kv := func(k string, v string, st ...lipgloss.Style) {
		val := sVal
		if len(st) > 0 {
			val = st[0]
		}
		b.WriteString("  " + sKey.Render(fmt.Sprintf("%-22s", k+":")) + " " + val.Render(v) + "\n")
	}
	section := func(t string) { b.WriteString("\n " + sSection.Render(t) + "\n") }
	bad := lipgloss.NewStyle().Foreground(cBad)
	warn := lipgloss.NewStyle().Foreground(cWarn)
	good := lipgloss.NewStyle().Foreground(cGood)

	section("Device")
	kv("Name", d.Name)
	kv("Primary user", orDash(d.User))
	kv("Model", strings.TrimSpace(d.Manufacturer+" "+d.Model))
	kv("Serial", orDash(d.Serial))
	kv("Windows", fmt.Sprintf("%s (%s)", Release(d.OSVersion), orDash(d.OSVersion)))
	kv("Join type", orDash(d.JoinType))
	kv("Autopilot", yesNo(d.AutopilotEnrolled))
	kv("Ownership", orDash(d.Ownership))
	kv("Site / category", orDash(d.Category))
	kv("Intune device ID", d.ID)
	kv("Entra device ID", orDash(d.EntraDeviceID))

	section("Health")
	st := good
	switch d.Compliance {
	case "noncompliant", "error", "conflict":
		st = bad
	case "inGracePeriod":
		st = warn
	}
	kv("Compliance", compliance(d.Compliance), st)
	for _, p := range d.FailingPolicies {
		kv("  failing", p, bad)
	}
	syncSt := sVal
	if !d.LastSync.IsZero() && now.Sub(d.LastSync) > 14*24*time.Hour {
		syncSt = lipgloss.NewStyle().Foreground(cStale)
	}
	kv("Last check-in", fmt.Sprintf("%s ago (%s)", ago(d.LastSync, now), d.LastSync.Local().Format("2006-01-02 15:04")), syncSt)
	kv("Enrolled", d.Enrolled.Local().Format("2006-01-02"))
	if d.Encrypted {
		kv("BitLocker", "encrypted", good)
	} else {
		kv("BitLocker", "not encrypted", bad)
	}

	section("Defender")
	if df := d.Defender; df != nil {
		dst := good
		if df.State != "clean" {
			dst = bad
		}
		kv("State", df.State, dst)
		if df.RealTime {
			kv("Real-time protection", "on", good)
		} else {
			kv("Real-time protection", "off", bad)
		}
		if df.SignaturesOverdue {
			kv("Signatures", "overdue ("+df.SignatureVersion+")", warn)
		} else {
			kv("Signatures", "up to date ("+df.SignatureVersion+")", good)
		}
		kv("Tamper protection", yesNo(df.TamperProtection))
		if !df.LastQuickScan.IsZero() {
			kv("Last quick scan", ago(df.LastQuickScan, now)+" ago")
		}
		if !df.LastReported.IsZero() {
			kv("Last reported", ago(df.LastReported, now)+" ago")
		}
	} else {
		kv("State", "no Defender status reported", sMuted)
	}

	section("Hardware")
	if d.TotalStorageGB > 0 {
		free := d.FreeStorageGB / d.TotalStorageGB
		fs := sVal
		if free < 0.1 {
			fs = bad
		} else if free < 0.2 {
			fs = warn
		}
		kv("Storage", fmt.Sprintf("%.0f GB free of %.0f GB  %s", d.FreeStorageGB, d.TotalStorageGB, bar(1-free, 20)), fs)
	}
	if d.PhysicalMemoryGB > 0 {
		kv("Memory", fmt.Sprintf("%.0f GB", d.PhysicalMemoryGB))
	}
	if d.WiFiMAC != "" {
		kv("Wi-Fi MAC", d.WiFiMAC)
	}
	return b.String()
}

// bar draws a small usage meter.
func bar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := int(frac*float64(width) + 0.5)
	return "[" + strings.Repeat("█", n) + strings.Repeat("░", width-n) + "]"
}

// i9s is a k9s-style terminal UI for Microsoft Intune.
//
//	i9s                 sign in and open your devices
//	i9s --demo          try it on a fictional tenant, no sign-in
//	i9s users           start on a resource (devices, users, apps, pulse)
//	i9s dv noncompliant start with a filter
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/YeomanLabs/i9s/internal/auth"
	"github.com/YeomanLabs/i9s/internal/demo"
	"github.com/YeomanLabs/i9s/internal/graph"
	"github.com/YeomanLabs/i9s/internal/intune"
	"github.com/YeomanLabs/i9s/internal/ui"
)

var version = "dev"

func main() {
	var (
		demoMode = flag.Bool("demo", false, "use a fictional tenant (no sign-in)")
		clientID = flag.String("client-id", "", "your own app registration's client ID (default: Microsoft Graph Command Line Tools)")
		tenantID = flag.String("tenant", "", "tenant ID or domain (default: your account's home tenant)")
		refresh  = flag.Duration("refresh", 60*time.Second, "auto-refresh interval for lists (0 to turn off)")
		signOut  = flag.Bool("sign-out", false, "forget the signed-in account and exit")
		showVer  = flag.Bool("version", false, "print the version and exit")
		check    = flag.Bool("check", false, "sign in, try each data source once, print what works, and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "i9s %s: a terminal UI for Microsoft Intune\n\nUsage:\n  i9s [flags] [resource] [filter]\n\nResources: devices (dv), users (us), apps (ap), pulse (pu)\n\nFlags:\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()
	ui.Version = version

	if *showVer {
		fmt.Println("i9s", version)
		return
	}
	opts := auth.Options{ClientID: *clientID, TenantID: *tenantID}
	if *signOut {
		if err := auth.SignOut(opts); err != nil {
			fail(err)
		}
		fmt.Println("Signed out.")
		return
	}

	var src intune.Source
	if *demoMode {
		src = demo.New()
	} else {
		fmt.Println("Signing in to Microsoft… (a browser tab opens the first time)")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		sess, err := auth.SignIn(ctx, opts, graph.ReadScopes)
		cancel()
		if err != nil {
			fail(fmt.Errorf("sign-in failed: %w", err))
		}
		c := graph.New(sess.Token)
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		name := graph.TenantName(ctx, c)
		cancel()
		src = graph.NewSource(c, name, sess.Account)
	}

	if *check {
		os.Exit(runCheck(src))
	}

	m := ui.New(src, ui.Options{Refresh: *refresh, Start: strings.Join(flag.Args(), " ")})
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fail(err)
	}
}

// runCheck is a non-interactive health check: one call per data source.
func runCheck(src intune.Source) int {
	fmt.Printf("i9s %s · %s · %s\n\n", version, src.Tenant(), src.Account())
	failed := 0
	step := func(name string, fn func(ctx context.Context) (string, error)) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		start := time.Now()
		msg, err := fn(ctx)
		took := time.Since(start).Round(10 * time.Millisecond)
		if err != nil {
			failed++
			fmt.Printf("  FAIL  %-18s %v\n", name, err)
			return
		}
		fmt.Printf("  ok    %-18s %s (%s)\n", name, msg, took)
	}

	var devices []intune.Device
	var apps []intune.App
	step("devices", func(ctx context.Context) (string, error) {
		d, err := src.Devices(ctx)
		devices = d
		return fmt.Sprintf("%d Windows devices", len(d)), err
	})
	step("device detail", func(ctx context.Context) (string, error) {
		if len(devices) == 0 {
			return "skipped (no devices)", nil
		}
		d, err := src.DeviceDetail(ctx, devices[0].ID)
		defender := "no Defender status"
		if d.Defender != nil {
			defender = "Defender " + d.Defender.State
		}
		return fmt.Sprintf("%s: %s, %.0f GB free", d.Name, defender, d.FreeStorageGB), err
	})
	step("users", func(ctx context.Context) (string, error) {
		u, err := src.Users(ctx)
		withMFA, withSignIn := 0, 0
		for _, x := range u {
			if x.MFA != "" {
				withMFA++
			}
			if !x.LastSignIn.IsZero() {
				withSignIn++
			}
		}
		return fmt.Sprintf("%d people (%d with MFA data, %d with sign-in activity)", len(u), withMFA, withSignIn), err
	})
	step("apps", func(ctx context.Context) (string, error) {
		a, err := src.Apps(ctx)
		apps = a
		return fmt.Sprintf("%d assigned Windows apps", len(a)), err
	})
	step("app install status", func(ctx context.Context) (string, error) {
		if len(apps) == 0 {
			return "skipped (no apps)", nil
		}
		s, err := src.AppStatus(ctx, apps[0].ID)
		return fmt.Sprintf("%s: %d devices", apps[0].Name, len(s)), err
	})
	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d check(s) failed.\n", failed)
		return 1
	}
	fmt.Println("Everything i9s reads is working.")
	return 0
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "i9s:", err)
	os.Exit(1)
}

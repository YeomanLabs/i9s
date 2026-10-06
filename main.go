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

	m := ui.New(src, ui.Options{Refresh: *refresh, Start: strings.Join(flag.Args(), " ")})
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "i9s:", err)
	os.Exit(1)
}

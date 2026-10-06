// Package auth signs in with Microsoft: authorization code + PKCE in the
// system browser, redirected to localhost. By default it uses Microsoft Graph
// Command Line Tools, Microsoft's own public client that exists in every
// tenant, so i9s needs no app registration. Tokens are cached encrypted (DPAPI
// on Windows, Keychain on macOS, libsecret on Linux) so you sign in once.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity/cache"
)

// GraphCLIClientID is Microsoft Graph Command Line Tools.
const GraphCLIClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

type Options struct {
	ClientID string // blank = Graph Command Line Tools
	TenantID string // blank = "organizations"
	Dir      string // where the authentication record lives
}

type Session struct {
	cred    *azidentity.InteractiveBrowserCredential
	Account string
	Tenant  string
}

// ConfigDir is %AppData%\i9s (Windows) or ~/.config/i9s.
func ConfigDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "i9s")
}

func scopesOf(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		if strings.HasPrefix(n, "https://") {
			out[i] = n
		} else {
			out[i] = "https://graph.microsoft.com/" + n
		}
	}
	return out
}

// SignIn reuses the cached account when there is one, otherwise opens the
// browser. readScopes are consented up front.
func SignIn(ctx context.Context, o Options, readScopes []string) (*Session, error) {
	if o.ClientID == "" {
		o.ClientID = GraphCLIClientID
	}
	if o.TenantID == "" {
		o.TenantID = "organizations"
	}
	if o.Dir == "" {
		o.Dir = ConfigDir()
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	recordFile := filepath.Join(o.Dir, "account-"+o.ClientID[:8]+".json")

	var record azidentity.AuthenticationRecord
	if b, err := os.ReadFile(recordFile); err == nil {
		_ = json.Unmarshal(b, &record)
	}

	c, err := cache.New(&cache.Options{Name: "i9s"})
	if err != nil {
		// No OS keychain available (e.g. headless Linux): keep tokens in memory only.
		c = azidentity.Cache{}
	}
	cred, err := azidentity.NewInteractiveBrowserCredential(&azidentity.InteractiveBrowserCredentialOptions{
		ClientID:             o.ClientID,
		TenantID:             o.TenantID,
		RedirectURL:          "http://localhost",
		Cache:                c,
		AuthenticationRecord: record,
		// Admin-consented scopes only work once requested; let new ones prompt.
		DisableAutomaticAuthentication: false,
	})
	if err != nil {
		return nil, err
	}

	if record == (azidentity.AuthenticationRecord{}) {
		record, err = cred.Authenticate(ctx, &policy.TokenRequestOptions{Scopes: scopesOf(readScopes)})
		if err != nil {
			return nil, friendly(err)
		}
		if b, err := json.MarshalIndent(record, "", "  "); err == nil {
			_ = os.WriteFile(recordFile, b, 0o600)
		}
		// Rebuild with the record so silent refresh finds the cached account.
		cred, err = azidentity.NewInteractiveBrowserCredential(&azidentity.InteractiveBrowserCredentialOptions{
			ClientID: o.ClientID, TenantID: o.TenantID, RedirectURL: "http://localhost", Cache: c, AuthenticationRecord: record,
		})
		if err != nil {
			return nil, err
		}
	}
	return &Session{cred: cred, Account: record.Username, Tenant: record.TenantID}, nil
}

// Token returns a Graph token for these scopes, prompting in the browser
// only if new consent is needed.
func (s *Session) Token(ctx context.Context, scopes []string) (string, error) {
	tok, err := s.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: scopesOf(scopes)})
	if err != nil {
		return "", friendly(err)
	}
	return tok.Token, nil
}

// SignOut forgets the cached account (tokens in the OS cache expire on their own).
func SignOut(o Options) error {
	if o.ClientID == "" {
		o.ClientID = GraphCLIClientID
	}
	if o.Dir == "" {
		o.Dir = ConfigDir()
	}
	err := os.Remove(filepath.Join(o.Dir, "account-"+o.ClientID[:8]+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func friendly(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "AADSTS65001") || strings.Contains(msg, "consent"):
		return errors.New("an admin needs to approve the permissions for Microsoft Graph Command Line Tools (or use --client-id with your own app registration)")
	case strings.Contains(msg, "AADSTS50105") || strings.Contains(msg, "AADSTS50011"):
		return errors.New("your tenant blocks Microsoft Graph Command Line Tools for this account; use --client-id with your own app registration")
	}
	return err
}

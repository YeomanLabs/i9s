package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(func(context.Context, []string) (string, error) { return "tok", nil })
	c.Base = srv.URL
	c.Sleep = func(time.Duration) {}
	return c
}

func TestAllFollowsNextLinkAndRetriesThrottling(t *testing.T) {
	calls := 0
	var c *Client
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer token")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1.0/things"):
			json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "a"}}, "@odata.nextLink": c.Base + "/v1.0/page2"})
		case strings.HasSuffix(r.URL.Path, "/page2") && calls == 2:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
		case strings.HasSuffix(r.URL.Path, "/page2"):
			json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "b"}}})
		}
	})
	got, err := All[struct{ ID string }](context.Background(), c, "things", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" || calls != 3 {
		t.Fatalf("got %+v after %d calls", got, calls)
	}
}

func TestQueriesAreEncoded(t *testing.T) {
	var raw string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.RequestURI
		json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
	})
	if _, err := All[struct{}](context.Background(), c, "deviceManagement/managedDevices?$filter=operatingSystem eq 'Windows'&$select=id", nil); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(raw, " '") || !strings.Contains(raw, "operatingSystem%20eq%20%27Windows%27") {
		t.Fatalf("query sent unencoded: %s", raw)
	}
}

func TestErrorsCarryGraphMessage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"code":"Authorization_RequestDenied","message":"Insufficient privileges"}}`)
	})
	err := c.Get(context.Background(), "beta/x?$select=y", nil, nil)
	ge, ok := err.(*Error)
	if !ok || !ge.Denied() || ge.Code != "Authorization_RequestDenied" || ge.Path != "/beta/x" {
		t.Fatalf("unexpected error %#v", err)
	}
}

func TestReportPagesAndPrefersLocalizedColumns(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["filter"] != "(ApplicationId eq 'x')" {
			t.Errorf("filter not passed: %v", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"TotalRowCount": 2,
			"Schema":        []map[string]string{{"Column": "DeviceId"}, {"Column": "AppInstallState"}, {"Column": "AppInstallState_loc"}},
			"Values":        [][]any{{"d1", 1, "Installed"}, {"d2", 2, "Failed"}},
		})
	})
	rows, err := Report(context.Background(), c, "act", nil, map[string]any{"filter": "(ApplicationId eq 'x')"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || Text(rows[1], "AppInstallState") != "Failed" || Text(rows[0], "DeviceId") != "d1" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestHelpers(t *testing.T) {
	cases := map[string]string{"Installed": "installed", "Install failed": "failed", "Pending install": "pending", "Not installed": "notInstalled", "Not applicable": "notApplicable", "weird": "unknown"}
	for in, want := range cases {
		if got := AppState(in); got != want {
			t.Errorf("AppState(%q) = %q, want %q", in, got, want)
		}
	}
	if MFAStrength(true, true, nil) != "passwordless" || MFAStrength(false, true, []string{"microsoftAuthenticatorPush"}) != "strong" ||
		MFAStrength(false, true, []string{"mobilePhone"}) != "weak" || MFAStrength(false, false, nil) != "none" {
		t.Error("MFAStrength grading is off")
	}
	if SkuName("SPE_E3") != "M365 E3" || SkuName("NEW_THING") != "NEW THING" {
		t.Error("SkuName")
	}
}

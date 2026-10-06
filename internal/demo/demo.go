// Package demo is a fictional tenant ("Contoso Manufacturing") so anyone can
// try i9s without signing in. Seeded, so it looks the same every run. Remote
// actions are simulated.
package demo

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YeomanLabs/i9s/internal/intune"
)

type Source struct {
	mu      sync.Mutex
	now     time.Time
	devices []intune.Device
	users   []intune.User
	apps    []intune.App
	status  map[string][]intune.AppDeviceStatus
	detail  map[string]intune.DeviceDetail
	// Latency makes the spinner visible, like a real tenant.
	Latency time.Duration
}

var (
	sites  = []struct{ name, code string; n int }{{"Milwaukee HQ", "MKE", 140}, {"Madison", "MSN", 70}, {"Chicago", "CHI", 64}, {"Minneapolis", "MSP", 52}, {"Remote", "RMT", 76}, {"Green Bay", "GRB", 34}, {"Dallas", "DAL", 22}}
	models = []struct{ make, model, kind string; w int }{{"Dell Inc.", "Latitude 7450", "LT", 30}, {"Dell Inc.", "Latitude 5440", "LT", 18}, {"HP", "EliteBook 840 G11", "LT", 16}, {"LENOVO", "ThinkPad T14 Gen 5", "LT", 12}, {"Microsoft Corporation", "Surface Laptop 7", "LT", 8}, {"Dell Inc.", "OptiPlex 7020", "DT", 11}, {"Dell Inc.", "Latitude 7420", "LT", 5}}
	first  = []string{"alex", "sam", "jordan", "taylor", "morgan", "casey", "riley", "jamie", "drew", "avery", "quinn", "reese", "parker", "rowan", "skyler", "devon", "emerson", "hayden", "kai", "logan", "marlo", "noel", "peyton", "sage"}
	last   = []string{"nguyen", "patel", "garcia", "kowalski", "schmidt", "johnson", "okafor", "larsen", "muller", "rossi", "kim", "olsen", "hernandez", "novak", "berg", "chen", "dubois", "fischer", "haas", "keller"}
	depts  = []string{"Sales", "Engineering", "Finance", "Operations", "Customer Support", "HR", "IT", "Marketing", "Manufacturing"}
)

// New builds the demo tenant as of now.
func New() *Source { return NewAt(time.Now()) }

func NewAt(now time.Time) *Source {
	r := rand.New(rand.NewSource(20261006))
	s := &Source{now: now, status: map[string][]intune.AppDeviceStatus{}, detail: map[string]intune.DeviceDetail{}, Latency: 350 * time.Millisecond}
	used := map[string]bool{}
	upns := map[string]int{}

	for _, site := range sites {
		for i := 0; i < site.n; i++ {
			m := pickModel(r)
			name := ""
			for name == "" || used[name] {
				name = fmt.Sprintf("%s-%s-%05d", site.code, m.kind, r.Intn(99999))
			}
			used[name] = true

			f, l := first[r.Intn(len(first))], last[r.Intn(len(last))]
			base := f + "." + l
			upns[base]++
			upn := base
			if upns[base] > 1 {
				upn = fmt.Sprintf("%s%d", base, upns[base])
			}
			upn += "@contoso.com"

			// Mostly synced in the last day, with a tail of travellers and forgotten machines.
			age := time.Duration(r.ExpFloat64()*8*float64(time.Hour)) + time.Duration(r.Intn(60))*time.Minute
			if t := r.Float64(); t < 0.07 {
				age = time.Duration(7+r.Intn(40)) * 24 * time.Hour
			} else if t < 0.1 {
				age = time.Duration(2+r.Intn(5)) * 24 * time.Hour
			}
			build := []string{"10.0.26100.6899", "10.0.26200.6899", "10.0.26100.6584", "10.0.22631.6060", "10.0.19045.6456"}[pickW(r, []int{38, 30, 14, 9, 9})]
			if site.code == "DAL" && r.Float64() < 0.3 {
				build = "10.0.19045.6332"
			}
			encrypted := r.Float64() > 0.03
			comp := "compliant"
			var failing []string
			if !encrypted {
				failing = append(failing, "BitLocker required")
			}
			if strings.Contains(build, "19045") && r.Float64() < 0.6 {
				failing = append(failing, "Minimum OS version")
			}
			if age > 30*24*time.Hour {
				failing = append(failing, "Device check-in")
			}
			if len(failing) > 0 {
				comp = "noncompliant"
				if age < 72*time.Hour && r.Float64() < 0.3 {
					comp = "inGracePeriod"
				}
			} else if r.Float64() < 0.01 {
				comp = "unknown"
			}
			id := fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", r.Uint32(), r.Intn(0xffff), r.Intn(0xfff), r.Intn(0xfff), r.Int63n(1<<48))
			d := intune.Device{
				ID: id, Name: name, User: upn, OSVersion: build, Manufacturer: m.make, Model: m.model,
				Serial: fmt.Sprintf("%07X", r.Int63n(0xFFFFFFF)), Compliance: comp, LastSync: now.Add(-age),
				Enrolled: now.Add(-time.Duration(30+r.Intn(900)) * 24 * time.Hour), Encrypted: encrypted, Ownership: "company",
				Category: site.name, EntraDeviceID: fmt.Sprintf("%08x-aaaa-4bbb-8ccc-%012x", r.Uint32(), r.Int63n(1<<48)), ManagementAgent: "mdm",
			}
			s.devices = append(s.devices, d)

			defState := "clean"
			if r.Float64() < 0.04 {
				defState = []string{"rebootPending", "fullScanPending", "critical"}[r.Intn(3)]
			}
			total := []float64{237.9, 475.8, 953.2}[r.Intn(3)]
			s.detail[id] = intune.DeviceDetail{
				Device:          d,
				FailingPolicies: failing,
				FreeStorageGB:   total * (0.05 + r.Float64()*0.6), TotalStorageGB: total,
				PhysicalMemoryGB: []float64{16, 16, 32, 8}[r.Intn(4)], WiFiMAC: fmt.Sprintf("%012X", r.Int63n(1<<48)),
				JoinType: []string{"azureADJoined", "hybridAzureADJoined"}[pickW(r, []int{70, 30})], AutopilotEnrolled: r.Float64() < 0.8,
				Defender: &intune.Defender{
					State: defState, RealTime: r.Float64() > 0.02, SignaturesOverdue: age > 3*24*time.Hour,
					SignatureVersion: fmt.Sprintf("1.459.%d.0", 200+r.Intn(120)), LastQuickScan: now.Add(-time.Duration(r.Intn(96)) * time.Hour),
					LastReported: d.LastSync, TamperProtection: r.Float64() > 0.1,
				},
			}

			s.users = append(s.users, intune.User{
				ID: fmt.Sprintf("u-%04d", len(s.users)), UPN: upn, Name: strings.Title(f) + " " + strings.Title(l), //nolint:staticcheck
				Department: depts[r.Intn(len(depts))], Office: site.name, Enabled: r.Float64() > 0.03,
				LastSignIn: now.Add(-time.Duration(r.ExpFloat64()*30) * time.Hour), MFA: []string{"passwordless", "strong", "weak", "none"}[pickW(r, []int{18, 62, 13, 7})],
				Licenses: []string{[]string{"M365 E3", "M365 E5", "M365 F3"}[pickW(r, []int{80, 8, 12})]},
			})
		}
	}
	sort.Slice(s.users, func(i, j int) bool { return s.users[i].UPN < s.users[j].UPN })

	appDefs := []struct{ name, kind, pub, ver string; fail float64 }{
		{"Microsoft 365 Apps for enterprise", "m365", "Microsoft", "2409", 0.012},
		{"Microsoft Teams", "win32", "Microsoft", "25255.703", 0.02},
		{"Company Portal", "store", "Microsoft", "11.2.1495", 0.005},
		{"Zoom Workplace", "win32", "Zoom", "6.2.5", 0.04},
		{"GlobalProtect VPN", "win32", "Palo Alto Networks", "6.3.1", 0.06},
		{"Adobe Acrobat Reader", "winget", "Adobe", "24.003", 0.015},
		{"7-Zip", "winget", "Igor Pavlov", "24.08", 0.003},
		{"Notepad++", "winget", "Don Ho", "8.7", 0.004},
		{"Microsoft Edge", "edge", "Microsoft", "129.0", 0.002},
		{"Visual Studio Code", "winget", "Microsoft", "1.94", 0.01},
		{"Dell Command Update", "win32", "Dell", "5.4", 0.08},
		{"Cisco Webex", "msi", "Cisco", "44.9", 0.03},
	}
	for i, a := range appDefs {
		app := intune.App{ID: fmt.Sprintf("app-%02d", i), Name: a.name, Type: a.kind, Publisher: a.pub, Version: a.ver}
		s.apps = append(s.apps, app)
		var rows []intune.AppDeviceStatus
		for _, d := range s.devices {
			if a.pub == "Dell" && d.Manufacturer != "Dell Inc." {
				continue
			}
			st, detail, code := "installed", "", ""
			switch x := r.Float64(); {
			case x < a.fail:
				st, code = "failed", []string{"0x80070643", "0x87D1041C", "0x80073CF9", "0x8007000D"}[r.Intn(4)]
				detail = map[string]string{"0x80070643": "Fatal error during installation", "0x87D1041C": "Application not detected after installation", "0x80073CF9": "Package install failed", "0x8007000D": "Invalid data"}[code]
			case x < a.fail+0.02:
				st, detail = "pending", "Waiting for install status"
			case x < a.fail+0.025:
				st = "notApplicable"
			}
			if now.Sub(d.LastSync) > 14*24*time.Hour {
				st, detail, code = "pending", "Device hasn't checked in", ""
			}
			rows = append(rows, intune.AppDeviceStatus{DeviceID: d.ID, DeviceName: d.Name, User: d.User, State: st, Detail: detail, ErrorCode: code})
		}
		s.status[app.ID] = rows
	}
	return s
}

func pickModel(r *rand.Rand) struct{ make, model, kind string; w int } {
	ws := make([]int, len(models))
	for i, m := range models {
		ws[i] = m.w
	}
	return models[pickW(r, ws)]
}

func pickW(r *rand.Rand, ws []int) int {
	total := 0
	for _, w := range ws {
		total += w
	}
	x := r.Intn(total)
	for i, w := range ws {
		if x < w {
			return i
		}
		x -= w
	}
	return len(ws) - 1
}

func (s *Source) wait(ctx context.Context) error {
	select {
	case <-time.After(s.Latency):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Source) Tenant() string  { return "Contoso Manufacturing" }
func (s *Source) Account() string { return "demo@contoso.com" }
func (s *Source) Demo() bool      { return true }

func (s *Source) Devices(ctx context.Context) ([]intune.Device, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]intune.Device(nil), s.devices...), nil
}

func (s *Source) Users(ctx context.Context) ([]intune.User, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	return append([]intune.User(nil), s.users...), nil
}

func (s *Source) Apps(ctx context.Context) ([]intune.App, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	return append([]intune.App(nil), s.apps...), nil
}

func (s *Source) AppStatus(ctx context.Context, appID string) ([]intune.AppDeviceStatus, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	return append([]intune.AppDeviceStatus(nil), s.status[appID]...), nil
}

func (s *Source) DeviceDetail(ctx context.Context, id string) (intune.DeviceDetail, error) {
	if err := s.wait(ctx); err != nil {
		return intune.DeviceDetail{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.detail[id]
	if !ok {
		return intune.DeviceDetail{}, fmt.Errorf("device %s not found", id)
	}
	for _, x := range s.devices {
		if x.ID == id {
			d.Device = x
		}
	}
	return d, nil
}

// Sync pretends the device checked in: its last sync becomes now.
func (s *Source) Sync(ctx context.Context, id string) error {
	if err := s.wait(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].ID == id {
			s.devices[i].LastSync = time.Now()
		}
	}
	return nil
}

func (s *Source) Restart(ctx context.Context, id string) error {
	return s.Sync(ctx, id)
}

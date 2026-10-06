// Package intune holds the data i9s shows and the Source interface that both
// the live Microsoft Graph backend and the offline demo implement.
package intune

import (
	"context"
	"time"
)

// Device is one Intune-managed Windows device.
type Device struct {
	ID              string
	Name            string
	User            string
	OSVersion       string
	Manufacturer    string
	Model           string
	Serial          string
	Compliance      string // compliant, noncompliant, inGracePeriod, unknown, ...
	LastSync        time.Time
	Enrolled        time.Time
	Encrypted       bool
	Ownership       string // company, personal, unknown
	Category        string
	EntraDeviceID   string
	ManagementAgent string
}

// User is one Entra ID member user.
type User struct {
	ID         string
	UPN        string
	Name       string
	Department string
	Office     string
	Enabled    bool
	LastSignIn time.Time // zero when unknown or never
	MFA        string    // passwordless, strong, weak, none, "" when unknown
	Licenses   []string
}

// App is an Intune app assignment target.
type App struct {
	ID        string
	Name      string
	Type      string // win32, winget, msi, m365, store, edge, ...
	Publisher string
	Version   string
}

// AppDeviceStatus is one device's install state for an app.
type AppDeviceStatus struct {
	DeviceID   string
	DeviceName string
	User       string
	State      string // installed, failed, pending, notApplicable, notInstalled, unknown
	Detail     string
	ErrorCode  string
}

// DeviceDetail is everything the describe view shows for one device.
type DeviceDetail struct {
	Device
	Defender          *Defender
	FailingPolicies   []string
	FreeStorageGB     float64
	TotalStorageGB    float64
	PhysicalMemoryGB  float64
	WiFiMAC           string
	JoinType          string
	AutopilotEnrolled bool
}

// Defender is the antivirus state Intune reports for a device.
type Defender struct {
	State             string // clean, critical, rebootPending, ...
	RealTime          bool
	SignaturesOverdue bool
	SignatureVersion  string
	LastQuickScan     time.Time
	LastReported      time.Time
	TamperProtection  bool
}

// Source is where i9s gets its data. Every method must be safe to call from a
// goroutine; the UI never blocks on them.
type Source interface {
	Tenant() string
	Account() string
	Demo() bool

	Devices(ctx context.Context) ([]Device, error)
	Users(ctx context.Context) ([]User, error)
	Apps(ctx context.Context) ([]App, error)
	AppStatus(ctx context.Context, appID string) ([]AppDeviceStatus, error)
	DeviceDetail(ctx context.Context, id string) (DeviceDetail, error)

	// Actions. Sync and Restart are remote device actions in Intune.
	Sync(ctx context.Context, deviceID string) error
	Restart(ctx context.Context, deviceID string) error
}

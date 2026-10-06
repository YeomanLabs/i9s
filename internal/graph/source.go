package graph

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YeomanLabs/i9s/internal/intune"
)

// Read scopes are requested at sign-in. The privileged scope for remote
// actions is only requested the first time you sync or restart a device.
var (
	ReadScopes = []string{
		"DeviceManagementManagedDevices.Read.All",
		"DeviceManagementConfiguration.Read.All",
		"DeviceManagementApps.Read.All",
		"User.Read.All",
		"AuditLog.Read.All",
	}
	ActionScopes = []string{"DeviceManagementManagedDevices.PrivilegedOperations.All"}
)

// Source is the live tenant, read through Microsoft Graph.
type Source struct {
	C       *Client
	tenant  string
	account string

	mu   sync.Mutex
	skus map[string]string
}

func NewSource(c *Client, tenant, account string) *Source {
	return &Source{C: c, tenant: tenant, account: account}
}

func (s *Source) Tenant() string  { return s.tenant }
func (s *Source) Account() string { return s.account }
func (s *Source) Demo() bool      { return false }

// TenantName looks up the organization's display name.
func TenantName(ctx context.Context, c *Client) string {
	var org struct {
		Value []struct {
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := c.Get(ctx, "organization?$select=displayName", ReadScopes, &org); err == nil && len(org.Value) > 0 {
		return org.Value[0].DisplayName
	}
	return "Your tenant"
}

type managedDevice struct {
	ID                        string  `json:"id"`
	DeviceName                string  `json:"deviceName"`
	UserPrincipalName         string  `json:"userPrincipalName"`
	OSVersion                 string  `json:"osVersion"`
	Manufacturer              string  `json:"manufacturer"`
	Model                     string  `json:"model"`
	SerialNumber              string  `json:"serialNumber"`
	ComplianceState           string  `json:"complianceState"`
	LastSyncDateTime          string  `json:"lastSyncDateTime"`
	EnrolledDateTime          string  `json:"enrolledDateTime"`
	IsEncrypted               bool    `json:"isEncrypted"`
	ManagedDeviceOwnerType    string  `json:"managedDeviceOwnerType"`
	DeviceCategoryDisplayName string  `json:"deviceCategoryDisplayName"`
	AzureADDeviceID           string  `json:"azureADDeviceId"`
	ManagementAgent           string  `json:"managementAgent"`
	OperatingSystem           string  `json:"operatingSystem"`
	FreeStorageSpaceInBytes   float64 `json:"freeStorageSpaceInBytes"`
	TotalStorageSpaceInBytes  float64 `json:"totalStorageSpaceInBytes"`
	PhysicalMemoryInBytes     float64 `json:"physicalMemoryInBytes"`
	WiFiMacAddress            string  `json:"wiFiMacAddress"`
	JoinType                  string  `json:"joinType"`
	AutopilotEnrolled         bool    `json:"autopilotEnrolled"`
}

const deviceSelect = "id,deviceName,userPrincipalName,osVersion,manufacturer,model,serialNumber,complianceState,lastSyncDateTime,enrolledDateTime,isEncrypted,managedDeviceOwnerType,deviceCategoryDisplayName,azureADDeviceId,managementAgent,operatingSystem"

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	if t.Year() < 2000 { // Graph uses 0001-01-01 for "never"
		return time.Time{}
	}
	return t
}

func (m managedDevice) toDevice() intune.Device {
	return intune.Device{
		ID: m.ID, Name: m.DeviceName, User: m.UserPrincipalName, OSVersion: m.OSVersion,
		Manufacturer: m.Manufacturer, Model: m.Model, Serial: m.SerialNumber, Compliance: m.ComplianceState,
		LastSync: parseTime(m.LastSyncDateTime), Enrolled: parseTime(m.EnrolledDateTime), Encrypted: m.IsEncrypted,
		Ownership: m.ManagedDeviceOwnerType, Category: m.DeviceCategoryDisplayName, EntraDeviceID: m.AzureADDeviceID,
		ManagementAgent: m.ManagementAgent,
	}
}

func (s *Source) Devices(ctx context.Context) ([]intune.Device, error) {
	// The operatingSystem filter isn't documented, so filter again client-side.
	raw, err := All[managedDevice](ctx, s.C, "deviceManagement/managedDevices?$filter=operatingSystem eq 'Windows'&$select="+deviceSelect, ReadScopes)
	if err != nil {
		return nil, err
	}
	out := make([]intune.Device, 0, len(raw))
	for _, m := range raw {
		if m.OperatingSystem != "" && !strings.EqualFold(m.OperatingSystem, "Windows") {
			continue
		}
		out = append(out, m.toDevice())
	}
	return out, nil
}

func (s *Source) DeviceDetail(ctx context.Context, id string) (intune.DeviceDetail, error) {
	var m managedDevice
	// Storage and memory only come back when selected explicitly; joinType and autopilotEnrolled are beta.
	sel := deviceSelect + ",freeStorageSpaceInBytes,totalStorageSpaceInBytes,physicalMemoryInBytes,wiFiMacAddress,joinType,autopilotEnrolled"
	if err := s.C.Get(ctx, "beta/deviceManagement/managedDevices/"+id+"?$select="+sel, ReadScopes, &m); err != nil {
		return intune.DeviceDetail{}, err
	}
	const gb = 1 << 30
	d := intune.DeviceDetail{
		Device: m.toDevice(), FreeStorageGB: m.FreeStorageSpaceInBytes / gb, TotalStorageGB: m.TotalStorageSpaceInBytes / gb,
		PhysicalMemoryGB: m.PhysicalMemoryInBytes / gb, WiFiMAC: m.WiFiMacAddress, JoinType: m.JoinType, AutopilotEnrolled: m.AutopilotEnrolled,
	}

	// Defender: read the navigation directly; $expand returns null on live tenants.
	var p struct {
		DeviceState            string `json:"deviceState"`
		RealTimeProtection     bool   `json:"realTimeProtectionEnabled"`
		SignatureUpdateOverdue bool   `json:"signatureUpdateOverdue"`
		SignatureVersion       string `json:"signatureVersion"`
		LastQuickScanDateTime  string `json:"lastQuickScanDateTime"`
		LastReportedDateTime   string `json:"lastReportedDateTime"`
		TamperProtection       *bool  `json:"tamperProtectionEnabled"`
	}
	if err := s.C.Get(ctx, "deviceManagement/managedDevices/"+id+"/windowsProtectionState", ReadScopes, &p); err == nil && p.DeviceState != "" {
		d.Defender = &intune.Defender{
			State: p.DeviceState, RealTime: p.RealTimeProtection, SignaturesOverdue: p.SignatureUpdateOverdue,
			SignatureVersion: p.SignatureVersion, LastQuickScan: parseTime(p.LastQuickScanDateTime), LastReported: parseTime(p.LastReportedDateTime),
			TamperProtection: p.TamperProtection != nil && *p.TamperProtection,
		}
	}

	// Failing compliance policies: the per-device endpoint is no longer
	// documented but still answers in most tenants. Best effort.
	if d.Compliance != "compliant" {
		type state struct {
			DisplayName string `json:"displayName"`
			State       string `json:"state"`
		}
		if states, err := All[state](ctx, s.C, "deviceManagement/managedDevices/"+id+"/deviceCompliancePolicyStates", ReadScopes); err == nil {
			for _, st := range states {
				if st.State == "nonCompliant" || st.State == "error" || st.State == "conflict" {
					d.FailingPolicies = append(d.FailingPolicies, st.DisplayName)
				}
			}
		}
	}
	return d, nil
}

func (s *Source) Users(ctx context.Context) ([]intune.User, error) {
	type graphUser struct {
		ID                string `json:"id"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
		Department        string `json:"department"`
		OfficeLocation    string `json:"officeLocation"`
		AccountEnabled    *bool  `json:"accountEnabled"`
		UserType          string `json:"userType"`
		AssignedLicenses  []struct {
			SkuID string `json:"skuId"`
		} `json:"assignedLicenses"`
		SignInActivity *struct {
			LastSignIn               string `json:"lastSignInDateTime"`
			LastNonInteractiveSignIn string `json:"lastNonInteractiveSignInDateTime"`
			LastSuccessfulSignIn     string `json:"lastSuccessfulSignInDateTime"`
		} `json:"signInActivity"`
	}
	base := "id,userPrincipalName,displayName,department,officeLocation,accountEnabled,userType,assignedLicenses"
	raw, err := All[graphUser](ctx, s.C, "users?$select="+base+",signInActivity&$top=500", ReadScopes)
	if ge, ok := err.(*Error); ok && ge.Denied() {
		// No Entra ID P1: load people without sign-in activity.
		raw, err = All[graphUser](ctx, s.C, "users?$select="+base+"&$top=999", ReadScopes)
	}
	if err != nil {
		return nil, err
	}

	mfa := map[string]string{}
	type reg struct {
		ID                    string   `json:"id"`
		IsPasswordlessCapable bool     `json:"isPasswordlessCapable"`
		IsMfaRegistered       bool     `json:"isMfaRegistered"`
		MethodsRegistered     []string `json:"methodsRegistered"`
	}
	if regs, err := All[reg](ctx, s.C, "reports/authenticationMethods/userRegistrationDetails", ReadScopes); err == nil {
		for _, r := range regs {
			mfa[r.ID] = MFAStrength(r.IsPasswordlessCapable, r.IsMfaRegistered, r.MethodsRegistered)
		}
	}
	skus := s.skuNames(ctx)

	out := make([]intune.User, 0, len(raw))
	for _, u := range raw {
		if u.UserType == "Guest" {
			continue
		}
		var last time.Time
		if a := u.SignInActivity; a != nil {
			for _, t := range []string{a.LastSignIn, a.LastNonInteractiveSignIn, a.LastSuccessfulSignIn} {
				if pt := parseTime(t); pt.After(last) {
					last = pt
				}
			}
		}
		var lic []string
		for _, l := range u.AssignedLicenses {
			if n, ok := skus[l.SkuID]; ok {
				lic = append(lic, n)
			} else {
				lic = append(lic, l.SkuID)
			}
		}
		sort.Strings(lic)
		out = append(out, intune.User{
			ID: u.ID, UPN: u.UserPrincipalName, Name: u.DisplayName, Department: u.Department, Office: u.OfficeLocation,
			Enabled: u.AccountEnabled == nil || *u.AccountEnabled, LastSignIn: last, MFA: mfa[u.ID], Licenses: lic,
		})
	}
	return out, nil
}

func (s *Source) skuNames(ctx context.Context) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skus != nil {
		return s.skus
	}
	s.skus = map[string]string{}
	type sku struct {
		SkuID         string `json:"skuId"`
		SkuPartNumber string `json:"skuPartNumber"`
	}
	// Reading SKUs needs Organization/LicenseAssignment read; fall back to IDs quietly.
	if list, err := All[sku](ctx, s.C, "subscribedSkus?$select=skuId,skuPartNumber", ReadScopes); err == nil {
		for _, k := range list {
			s.skus[k.SkuID] = SkuName(k.SkuPartNumber)
		}
	}
	return s.skus
}

var windowsAppTypes = map[string]string{
	"#microsoft.graph.win32LobApp":                  "win32",
	"#microsoft.graph.win32CatalogApp":              "win32",
	"#microsoft.graph.winGetApp":                    "winget",
	"#microsoft.graph.windowsMobileMSI":             "msi",
	"#microsoft.graph.officeSuiteApp":               "m365",
	"#microsoft.graph.windowsMicrosoftEdgeApp":      "edge",
	"#microsoft.graph.windowsUniversalAppX":         "appx",
	"#microsoft.graph.windowsAppX":                  "appx",
	"#microsoft.graph.windowsStoreApp":              "store",
	"#microsoft.graph.microsoftStoreForBusinessApp": "store",
}

func (s *Source) Apps(ctx context.Context) ([]intune.App, error) {
	type app struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Type        string `json:"@odata.type"`
		Publisher   string `json:"publisher"`
		Version     string `json:"displayVersion"`
	}
	raw, err := All[app](ctx, s.C, "beta/deviceAppManagement/mobileApps?$filter=isAssigned eq true", ReadScopes)
	if err != nil {
		return nil, err
	}
	var out []intune.App
	for _, a := range raw {
		if t, ok := windowsAppTypes[a.Type]; ok {
			out = append(out, intune.App{ID: a.ID, Name: a.DisplayName, Type: t, Publisher: a.Publisher, Version: a.Version})
		}
	}
	return out, nil
}

func (s *Source) AppStatus(ctx context.Context, appID string) ([]intune.AppDeviceStatus, error) {
	rows, err := Report(ctx, s.C, "microsoft.graph.retrieveDeviceAppInstallationStatusReport", ReadScopes, map[string]any{
		"select":  []string{"DeviceId", "DeviceName", "UserPrincipalName", "AppInstallState", "AppInstallStateDetails", "HexErrorCode"},
		"filter":  fmt.Sprintf("(ApplicationId eq '%s')", appID),
		"orderBy": []string{},
	})
	if err != nil {
		return nil, err
	}
	out := make([]intune.AppDeviceStatus, 0, len(rows))
	for _, r := range rows {
		out = append(out, intune.AppDeviceStatus{
			DeviceID: Text(r, "DeviceId"), DeviceName: Text(r, "DeviceName"), User: Text(r, "UserPrincipalName"),
			State: AppState(Text(r, "AppInstallState")), Detail: Text(r, "AppInstallStateDetails"), ErrorCode: Text(r, "HexErrorCode"),
		})
	}
	return out, nil
}

func (s *Source) Sync(ctx context.Context, id string) error {
	return s.C.Do(ctx, "POST", "deviceManagement/managedDevices/"+id+"/syncDevice", ActionScopes, nil, nil)
}

func (s *Source) Restart(ctx context.Context, id string) error {
	return s.C.Do(ctx, "POST", "deviceManagement/managedDevices/"+id+"/rebootNow", ActionScopes, nil, nil)
}

// AppState normalizes the report's install state text.
func AppState(raw string) string {
	s := strings.ToLower(strings.ReplaceAll(raw, " ", ""))
	switch {
	case strings.HasPrefix(s, "installed") || s == "success":
		return "installed"
	case strings.Contains(s, "fail") || strings.Contains(s, "error"):
		return "failed"
	case strings.Contains(s, "pending") || strings.Contains(s, "progress") || strings.Contains(s, "download"):
		return "pending"
	case strings.Contains(s, "notinstalled"):
		return "notInstalled"
	case strings.Contains(s, "notapplicable") || strings.Contains(s, "excluded"):
		return "notApplicable"
	}
	return "unknown"
}

var strongMethods = map[string]bool{
	"microsoftAuthenticatorPush": true, "softwareOneTimePasscode": true, "hardwareOneTimePasscode": true, "fido2": true,
	"windowsHelloForBusiness": true, "passKeyDeviceBound": true, "passKeyDeviceBoundAuthenticator": true,
	"passKeyDeviceBoundWindowsHello": true, "microsoftAuthenticatorPasswordless": true,
}

// MFAStrength grades a user's registered methods.
func MFAStrength(passwordless, registered bool, methods []string) string {
	if passwordless {
		return "passwordless"
	}
	weak := false
	for _, m := range methods {
		if strongMethods[m] {
			return "strong"
		}
		weak = true
	}
	if weak || registered {
		return "weak"
	}
	return "none"
}

var skuNames = map[string]string{
	"SPE_E3": "M365 E3", "SPE_E5": "M365 E5", "SPE_F1": "M365 F3", "SPB": "M365 Business Premium",
	"O365_BUSINESS_PREMIUM": "M365 Business Standard", "O365_BUSINESS_ESSENTIALS": "M365 Business Basic",
	"ENTERPRISEPACK": "O365 E3", "ENTERPRISEPREMIUM": "O365 E5", "STANDARDPACK": "O365 E1", "DESKLESSPACK": "O365 F3",
	"EMS": "EMS E3", "EMSPREMIUM": "EMS E5", "AAD_PREMIUM": "Entra ID P1", "AAD_PREMIUM_P2": "Entra ID P2",
	"INTUNE_A": "Intune P1", "WIN_DEF_ATP": "Defender for Endpoint", "POWER_BI_PRO": "Power BI Pro",
	"FLOW_FREE": "Power Automate Free", "POWER_BI_STANDARD": "Power BI Free", "Microsoft_365_Copilot": "M365 Copilot",
	"TEAMS_EXPLORATORY": "Teams Exploratory", "Microsoft_Teams_Premium": "Teams Premium", "VISIOCLIENT": "Visio P2",
}

// SkuName gives a short friendly name for a license part number.
func SkuName(part string) string {
	if n, ok := skuNames[part]; ok {
		return n
	}
	return strings.ReplaceAll(part, "_", " ")
}

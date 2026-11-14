package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

const psScript = `
$ErrorActionPreference = 'SilentlyContinue'
$adapters = Get-NetIPConfiguration | Where-Object {
    ($_.NetAdapter.Status -eq 'Up') -and
    ($_.IPv4Address.IPAddress -ne $null -or $_.IPv6Address.IPAddress -ne $null)
}
$out = foreach ($a in $adapters) {
    [pscustomobject]@{
        Alias     = $a.InterfaceAlias
        Desc      = $a.InterfaceDescription
        Status    = [string]$a.NetAdapter.Status
        Media     = [string]$a.NetAdapter.PhysicalMediaType
        Mac       = [string]$a.NetAdapter.MacAddress
        LinkSpeed = [string]$a.NetAdapter.LinkSpeed
        IPv4      = @($a.IPv4Address.IPAddress)
        IPv6      = @($a.IPv6Address.IPAddress)
        Gateway   = @($a.IPv4DefaultGateway.NextHop)
        DNS       = @($a.DNSServer.ServerAddresses)
    }
}
ConvertTo-Json -InputObject @($out) -Depth 4 -Compress
`

var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.\-:]*[A-Za-z0-9])?$`)
	timeRe = regexp.MustCompile(`(?i)time\s*([=<])\s*([\d.,]+)\s*ms`)
	lossRe = regexp.MustCompile(`(?i)timed out|unreachable|could not find|general failure|transmit failed|ttl expired`)
	wifiRe = regexp.MustCompile(`(?i)native 802\.11|wireless|wi-?fi`)
)

// WiFi holds the details parsed from `netsh wlan show interfaces`.
type WiFi struct {
	SSID         string `json:"ssid"`
	Standard     string `json:"standard"`
	Band         string `json:"band"`
	Channel      string `json:"channel"`
	ChannelWidth string `json:"channelWidth,omitempty"`
	RxRate       string `json:"rxRate"`
	TxRate       string `json:"txRate"`
	Signal       string `json:"signal"`
	SignalDbm    string `json:"signalDbm,omitempty"`
	Auth         string `json:"auth"`
}

// Adapter is one active network interface.
type Adapter struct {
	Alias     string   `json:"alias"`
	Desc      string   `json:"desc"`
	Status    string   `json:"status"`
	Media     string   `json:"media"`
	Mac       string   `json:"mac"`
	LinkSpeed string   `json:"linkSpeed"`
	IPv4      []string `json:"ipv4"`
	IPv6      []string `json:"ipv6"`
	Gateway   []string `json:"gateway"`
	DNS       []string `json:"dns"`
	IsWiFi    bool     `json:"isWifi"`
	WiFi      *WiFi    `json:"wifi,omitempty"`
}

// App is bound to the frontend.
type App struct {
	ctx             context.Context
	mu              sync.Mutex
	cancel          context.CancelFunc
	speedtestMu     sync.Mutex
	speedtestCancel context.CancelFunc
	speedtestCmd    *exec.Cmd
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(ctx context.Context) {
	a.StopPing()
	a.StopSpeedtest()
}

// ---------------------------------------------------------------- adapters

func clean(in []string) []string {
	out := []string{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func wifiGeneration(radio string) string {
	table := []struct{ re, label string }{
		{`802\.11be`, "Wi-Fi 7 (802.11be)"},
		{`802\.11ax`, "Wi-Fi 6 / 6E (802.11ax)"},
		{`802\.11ac`, "Wi-Fi 5 (802.11ac)"},
		{`802\.11n`, "Wi-Fi 4 (802.11n)"},
		{`802\.11g`, "802.11g (Legacy)"},
		{`802\.11b`, "802.11b (Legacy)"},
		{`802\.11a$`, "802.11a (Legacy)"},
	}
	for _, t := range table {
		if regexp.MustCompile(t.re).MatchString(radio) {
			return t.label
		}
	}
	if radio == "" {
		return "Unknown"
	}
	return radio
}

func wlanInfo() *WiFi {
	if native := nativeWlanInfo(); native != nil {
		return native
	}
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	hideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return nil
	}
	text := string(raw)
	grab := func(label string) string {
		m := regexp.MustCompile(`(?m)^\s*` + label + `\s*:\s*(.+)$`).FindStringSubmatch(text)
		if len(m) < 2 {
			return ""
		}
		return strings.TrimSpace(m[1])
	}
	radio := grab(`Radio type`)
	bssid := grab(`AP BSSID`)
	if bssid == "" {
		bssid = grab(`BSSID`)
	}
	ssid := grab(`SSID`)
	signal := grab(`Signal`)
	rawRssi := grab(`Rssi`)

	signalDbm := ""
	if rawRssi != "" {
		cleanRssi := strings.TrimSpace(rawRssi)
		if !strings.HasSuffix(strings.ToLower(cleanRssi), "dbm") {
			signalDbm = cleanRssi + " dBm"
		} else {
			signalDbm = cleanRssi
		}
	} else if signal != "" {
		pctStr := strings.TrimSpace(strings.TrimSuffix(signal, "%"))
		if pct, err := strconv.Atoi(pctStr); err == nil {
			signalDbm = fmt.Sprintf("%d dBm", (pct/2)-100)
		}
	}

	w := &WiFi{
		SSID:         ssid,
		Standard:     wifiGeneration(radio),
		Band:         grab(`Band`),
		Channel:      grab(`Channel`),
		ChannelWidth: queryChannelWidth(bssid, ssid),
		RxRate:       grab(`Receive rate \(Mbps\)`),
		TxRate:       grab(`Transmit rate \(Mbps\)`),
		Signal:       signal,
		SignalDbm:    signalDbm,
		Auth:         grab(`Authentication`),
	}
	if w.SSID == "" && radio == "" {
		return nil
	}
	return w
}

func formatSpeed(bps uint64) string {
	if bps >= 1_000_000_000 {
		return fmt.Sprintf("%.0f Gbps", float64(bps)/1_000_000_000)
	} else if bps >= 1_000_000 {
		return fmt.Sprintf("%.0f Mbps", float64(bps)/1_000_000)
	} else if bps >= 1_000 {
		return fmt.Sprintf("%.0f Kbps", float64(bps)/1_000)
	}
	return ""
}

func (a *App) getWin32Adapters() ([]Adapter, error) {
	var wifiInfo *WiFi
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wifiInfo = wlanInfo()
	}()

	flags := uint32(windows.GAA_FLAG_INCLUDE_GATEWAYS | windows.GAA_FLAG_INCLUDE_PREFIX)
	size := uint32(16384)
	buf := make([]byte, size)

	for {
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err == windows.ERROR_BUFFER_OVERFLOW {
			buf = make([]byte, size)
			continue
		}
		wg.Wait()
		return nil, err
	}

	wg.Wait()

	var result []Adapter
	cur := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))

	for cur != nil {
		alias := windows.UTF16PtrToString(cur.FriendlyName)
		desc := windows.UTF16PtrToString(cur.Description)
		isUp := cur.OperStatus == windows.IfOperStatusUp

		var ipv4, ipv6, gateways, dnsList []string

		u := cur.FirstUnicastAddress
		for u != nil {
			ip := u.Address.IP()
			if ip != nil && !ip.IsLoopback() && !ip.IsMulticast() {
				if v4 := ip.To4(); v4 != nil {
					ipv4 = append(ipv4, v4.String())
				} else if v6 := ip.To16(); v6 != nil {
					ipv6 = append(ipv6, v6.String())
				}
			}
			u = u.Next
		}

		g := cur.FirstGatewayAddress
		for g != nil {
			ip := g.Address.IP()
			if ip != nil && !ip.IsLoopback() && !ip.IsMulticast() {
				if v4 := ip.To4(); v4 != nil {
					gateways = append(gateways, v4.String())
				} else if v6 := ip.To16(); v6 != nil {
					gateways = append(gateways, v6.String())
				}
			}
			g = g.Next
		}

		d := cur.FirstDnsServerAddress
		for d != nil {
			ip := d.Address.IP()
			if ip != nil && !ip.IsLoopback() && !ip.IsMulticast() {
				if v4 := ip.To4(); v4 != nil {
					dnsList = append(dnsList, v4.String())
				} else if v6 := ip.To16(); v6 != nil {
					dnsList = append(dnsList, v6.String())
				}
			}
			d = d.Next
		}

		var macStr string
		if cur.PhysicalAddressLength > 0 && cur.PhysicalAddressLength <= 8 {
			var parts []string
			for i := 0; i < int(cur.PhysicalAddressLength); i++ {
				parts = append(parts, fmt.Sprintf("%02X", cur.PhysicalAddress[i]))
			}
			macStr = strings.Join(parts, "-")
		}

		speed := formatSpeed(cur.ReceiveLinkSpeed)
		if speed == "" {
			speed = formatSpeed(cur.TransmitLinkSpeed)
		}

		isWiFi := (cur.IfType == 71) || wifiRe.MatchString(alias+" "+desc)
		media := "802.3"
		if isWiFi {
			media = "Native 802.11"
		}

		if isUp && (len(ipv4) > 0 || len(ipv6) > 0) {
			ad := Adapter{
				Alias:     alias,
				Desc:      desc,
				Status:    "Up",
				Media:     media,
				Mac:       macStr,
				LinkSpeed: speed,
				IPv4:      clean(ipv4),
				IPv6:      clean(ipv6),
				Gateway:   clean(gateways),
				DNS:       clean(dnsList),
				IsWiFi:    isWiFi,
			}
			if isWiFi && wifiInfo != nil {
				ad.WiFi = wifiInfo
			}
			result = append(result, ad)
		}

		cur = cur.Next
	}

	return result, nil
}

// GetAdapters returns every UP adapter that has an IP address.
func (a *App) GetAdapters() ([]Adapter, error) {
	if goruntime.GOOS != "windows" {
		return nil, errors.New("this tool needs Windows")
	}

	// 1. Fast path: Native Win32 API (~10-15ms)
	if list, err := a.getWin32Adapters(); err == nil && len(list) > 0 {
		return list, nil
	}

	// 2. Fallback path: PowerShell query
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	hideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return nil, errors.New("PowerShell query failed: " + err.Error())
	}

	body := strings.TrimSpace(strings.TrimPrefix(string(raw), "\ufeff"))
	if body == "" {
		return []Adapter{}, nil
	}
	var parsed []*Adapter
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		var single Adapter
		if err2 := json.Unmarshal([]byte(body), &single); err2 != nil {
			return nil, errors.New("could not parse adapter data: " + err.Error())
		}
		parsed = []*Adapter{&single}
	}

	var wifi *WiFi
	wifiLoaded := false
	result := []Adapter{}
	for _, ad := range parsed {
		if ad == nil || ad.Alias == "" {
			continue
		}
		ad.IPv4, ad.IPv6 = clean(ad.IPv4), clean(ad.IPv6)
		ad.Gateway, ad.DNS = clean(ad.Gateway), clean(ad.DNS)
		ad.IsWiFi = wifiRe.MatchString(ad.Media + " " + ad.Alias)
		if ad.IsWiFi {
			if !wifiLoaded {
				wifi, wifiLoaded = wlanInfo(), true
			}
			ad.WiFi = wifi
		}
		result = append(result, *ad)
	}
	return result, nil
}

// -------------------------------------------------------------------- ping

// StartPing launches a ping and streams events to the UI:
// ping:line, ping:reply (ms), ping:loss, ping:done.
// count <= 0 means continuous.
func (a *App) StartPing(host string, count int) error {
	host = strings.TrimSpace(host)
	if !hostRe.MatchString(host) {
		return errors.New("enter a valid IP address or hostname")
	}
	if count > 100 {
		count = 100
	}

	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("a ping is already running")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.mu.Unlock()

	go a.runPing(ctx, host, count)
	return nil
}

func (a *App) runPing(ctx context.Context, host string, count int) {
	win := goruntime.GOOS == "windows"
	var args []string
	switch {
	case count <= 0 && win:
		args = []string{"-t", host}
	case count <= 0:
		args = []string{host}
	case win:
		args = []string{"-n", strconv.Itoa(count), host}
	default:
		args = []string{"-c", strconv.Itoa(count), host}
	}

	received := 0
	cmd := exec.CommandContext(ctx, "ping", args...)
	hideWindow(cmd)

	if out, err := cmd.StdoutPipe(); err != nil {
		wr.EventsEmit(a.ctx, "ping:line", "Error: "+err.Error())
	} else if err := cmd.Start(); err != nil {
		wr.EventsEmit(a.ctx, "ping:line", "Error: "+err.Error())
	} else {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r\n")
			if strings.TrimSpace(line) == "" {
				continue
			}
			wr.EventsEmit(a.ctx, "ping:line", line)
			if m := timeRe.FindStringSubmatch(line); m != nil {
				ms := 0.5 // "time<1ms"
				if m[1] == "=" {
					ms, _ = strconv.ParseFloat(strings.Replace(m[2], ",", ".", 1), 64)
				}
				received++
				wr.EventsEmit(a.ctx, "ping:reply", ms)
			} else if lossRe.MatchString(line) {
				wr.EventsEmit(a.ctx, "ping:loss", line)
			}
		}
		_ = cmd.Wait()
	}

	cancelled := ctx.Err() != nil
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.mu.Unlock()

	wr.EventsEmit(a.ctx, "ping:done", map[string]interface{}{
		"host":      host,
		"ok":        received > 0,
		"cancelled": cancelled,
	})
}

// StopPing cancels a running ping.
func (a *App) StopPing() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// ------------------------------------------------------------ window / misc

func (a *App) CopyText(s string) error { return wr.ClipboardSetText(a.ctx, s) }
func (a *App) Minimise()               { wr.WindowMinimise(a.ctx) }
func (a *App) ToggleMaximise()         { wr.WindowToggleMaximise(a.ctx) }
func (a *App) Quit()                   { a.StopPing(); wr.Quit(a.ctx) }

// ------------------------------------------------------------- Common directories & PLDT router
const (
	appFolderName     = "NetworkInspector"
	defaultRouterURL  = "http://192.168.1.1/html/login_pldt.html"
	pldtLocationsFile = "pldt_locations.json"
	dnsPresetsFile    = "dns_presets.json"
)

type LocationData struct {
	Username string `json:"username"`
	Password string `json:"password"`
	URL      string `json:"url"`
}

type RouterLocation struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	URL      string `json:"url"`
}

func defaultLocations() map[string]LocationData {
	return map[string]LocationData{}
}

func getAppDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("USERPROFILE")
	}

	var docsDir string
	if home != "" {
		docsDir = filepath.Join(home, "Documents")
	}
	if docsDir == "" {
		docsDir = "."
	}

	appDir := filepath.Join(docsDir, appFolderName)
	_ = os.MkdirAll(appDir, 0755)
	return appDir
}

func getLocationsFilePath() string {
	appDir := getAppDir()
	targetPath := filepath.Join(appDir, pldtLocationsFile)

	// Migration: if target doesn't exist yet, migrate from local file if present
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if localData, err := os.ReadFile(pldtLocationsFile); err == nil && len(localData) > 0 {
			_ = os.WriteFile(targetPath, localData, 0644)
		}
	}

	return targetPath
}

func getDNSPresetsFilePath() string {
	return filepath.Join(getAppDir(), dnsPresetsFile)
}

func loadLocations() map[string]LocationData {
	p := getLocationsFilePath()
	data, err := os.ReadFile(p)
	if err != nil {
		return defaultLocations()
	}
	var res map[string]LocationData
	if err := json.Unmarshal(data, &res); err != nil || len(res) == 0 {
		return defaultLocations()
	}
	return res
}

func saveLocations(data map[string]LocationData) error {
	p := getLocationsFilePath()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0644)
}

// GetRouterLocations returns all saved router locations.
func (a *App) GetRouterLocations() []RouterLocation {
	m := loadLocations()
	res := make([]RouterLocation, 0, len(m))
	for name, val := range m {
		u := val.URL
		if strings.TrimSpace(u) == "" {
			u = defaultRouterURL
		}
		res = append(res, RouterLocation{
			Name:     name,
			Username: val.Username,
			Password: val.Password,
			URL:      u,
		})
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})
	return res
}

// SaveRouterLocation adds or updates a router location.
func (a *App) SaveRouterLocation(loc RouterLocation) ([]RouterLocation, error) {
	name := strings.TrimSpace(loc.Name)
	if name == "" {
		return nil, errors.New("location name is required")
	}
	u := strings.TrimSpace(loc.URL)
	if u == "" {
		u = defaultRouterURL
	}
	m := loadLocations()
	m[name] = LocationData{
		Username: strings.TrimSpace(loc.Username),
		Password: loc.Password,
		URL:      u,
	}
	if err := saveLocations(m); err != nil {
		return nil, err
	}
	return a.GetRouterLocations(), nil
}

// DeleteRouterLocation removes a location by name.
func (a *App) DeleteRouterLocation(name string) ([]RouterLocation, error) {
	name = strings.TrimSpace(name)
	m := loadLocations()
	delete(m, name)
	if len(m) == 0 {
		m = defaultLocations()
	}
	if err := saveLocations(m); err != nil {
		return nil, err
	}
	return a.GetRouterLocations(), nil
}

// LaunchRouterPortal opens the router URL in the user's default web browser and copies the password to clipboard.
func (a *App) LaunchRouterPortal(name string) (map[string]interface{}, error) {
	m := loadLocations()
	loc, ok := m[name]
	if !ok {
		return nil, errors.New("location not found")
	}
	u := loc.URL
	if strings.TrimSpace(u) == "" {
		u = defaultRouterURL
	}

	_ = wr.ClipboardSetText(a.ctx, loc.Password)

	wr.BrowserOpenURL(a.ctx, u)

	return map[string]interface{}{
		"name":           name,
		"url":            u,
		"username":       loc.Username,
		"passwordCopied": true,
	}, nil
}

// GetAutoLoginScript returns the JavaScript auto-login snippet for bookmarklets or console injection.
func (a *App) GetAutoLoginScript(username, password string) string {
	return fmt.Sprintf(`javascript:(function(){var attempts=0;function tryLogin(){attempts++;var u=document.getElementById('user_name')||document.querySelector('input[type="text"]');var p=document.getElementById('loginpp')||document.querySelector('input[type="password"]');var b=document.getElementById('login_btn')||document.querySelector('button[type="submit"]');if(!u||!p){if(attempts<20)setTimeout(tryLogin,300);return;}u.value='%s';u.dispatchEvent(new Event('input',{bubbles:true}));p.value='%s';p.dispatchEvent(new Event('input',{bubbles:true}));if(typeof onlogin==='function'){onlogin(1);}else if(b){b.click();}}tryLogin();})();`,
		template.JSEscapeString(username),
		template.JSEscapeString(password),
	)
}

// ------------------------------------------------------------- DNS & IPConfig Management

// DNSPreset represents a DNS configuration preset (DHCP, public provider, or custom).
type DNSPreset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Primary     string `json:"primary"`
	Secondary   string `json:"secondary"`
	DohTemplate string `json:"dohTemplate"`
	IsDHCP      bool   `json:"isDhcp"`
	IsCustom    bool   `json:"isCustom"`
}

func defaultDNSPresets() []DNSPreset {
	return []DNSPreset{
		{
			ID:          "dhcp",
			Name:        "Automatic (DHCP)",
			Primary:     "",
			Secondary:   "",
			DohTemplate: "",
			IsDHCP:      true,
			IsCustom:    false,
		},
		{
			ID:          "cloudflare",
			Name:        "Cloudflare (1.1.1.1)",
			Primary:     "1.1.1.1",
			Secondary:   "1.0.0.1",
			DohTemplate: "https://cloudflare-dns.com/dns-query",
			IsDHCP:      false,
			IsCustom:    false,
		},
		{
			ID:          "google",
			Name:        "Google Public DNS",
			Primary:     "8.8.8.8",
			Secondary:   "8.8.4.4",
			DohTemplate: "https://dns.google/dns-query",
			IsDHCP:      false,
			IsCustom:    false,
		},
		{
			ID:          "quad9",
			Name:        "Quad9 (Malware Blocking)",
			Primary:     "9.9.9.9",
			Secondary:   "149.112.112.112",
			DohTemplate: "https://dns.quad9.net/dns-query",
			IsDHCP:      false,
			IsCustom:    false,
		},
		{
			ID:          "adguard",
			Name:        "AdGuard DNS (Ad Blocking)",
			Primary:     "94.140.14.14",
			Secondary:   "94.140.15.15",
			DohTemplate: "https://dns.adguard-dns.com/dns-query",
			IsDHCP:      false,
			IsCustom:    false,
		},
		{
			ID:          "opendns",
			Name:        "Cisco OpenDNS",
			Primary:     "208.67.222.222",
			Secondary:   "208.67.220.220",
			DohTemplate: "https://doh.opendns.com/dns-query",
			IsDHCP:      false,
			IsCustom:    false,
		},
		{
			ID:          "cleanbrowsing",
			Name:        "CleanBrowsing (Family)",
			Primary:     "185.228.168.168",
			Secondary:   "185.228.169.168",
			DohTemplate: "https://doh.cleanbrowsing.org/doh/family-filter/",
			IsDHCP:      false,
			IsCustom:    false,
		},
	}
}

func loadCustomPresets() []DNSPreset {
	p := getDNSPresetsFilePath()
	data, err := os.ReadFile(p)
	if err != nil {
		return []DNSPreset{}
	}
	var res []DNSPreset
	if err := json.Unmarshal(data, &res); err != nil {
		return []DNSPreset{}
	}
	return res
}

func saveCustomPresets(presets []DNSPreset) error {
	p := getDNSPresetsFilePath()
	b, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0644)
}

// GetDNSPresets returns all default and user-saved DNS presets.
func (a *App) GetDNSPresets() []DNSPreset {
	presets := defaultDNSPresets()
	custom := loadCustomPresets()
	presets = append(presets, custom...)
	return presets
}

// SaveCustomDNSPreset saves or updates a custom DNS preset.
func (a *App) SaveCustomDNSPreset(preset DNSPreset) ([]DNSPreset, error) {
	name := strings.TrimSpace(preset.Name)
	if name == "" {
		return nil, errors.New("preset name is required")
	}
	if !preset.IsDHCP {
		primary := strings.TrimSpace(preset.Primary)
		if primary == "" {
			return nil, errors.New("primary DNS server address is required")
		}
		if net.ParseIP(primary) == nil {
			return nil, errors.New("invalid primary IP address")
		}
		if sec := strings.TrimSpace(preset.Secondary); sec != "" {
			if net.ParseIP(sec) == nil {
				return nil, errors.New("invalid secondary IP address")
			}
		}
	}

	doh := strings.TrimSpace(preset.DohTemplate)
	if doh != "" && !strings.HasPrefix(strings.ToLower(doh), "https://") {
		doh = "https://" + doh
	}

	custom := loadCustomPresets()
	id := strings.TrimSpace(preset.ID)
	if id == "" {
		id = fmt.Sprintf("custom-%d", time.Now().UnixNano())
	}

	updated := false
	for i, c := range custom {
		if c.ID == id || strings.EqualFold(c.Name, name) {
			custom[i] = DNSPreset{
				ID:          id,
				Name:        name,
				Primary:     strings.TrimSpace(preset.Primary),
				Secondary:   strings.TrimSpace(preset.Secondary),
				DohTemplate: doh,
				IsDHCP:      preset.IsDHCP,
				IsCustom:    true,
			}
			updated = true
			break
		}
	}
	if !updated {
		custom = append(custom, DNSPreset{
			ID:          id,
			Name:        name,
			Primary:     strings.TrimSpace(preset.Primary),
			Secondary:   strings.TrimSpace(preset.Secondary),
			DohTemplate: doh,
			IsDHCP:      preset.IsDHCP,
			IsCustom:    true,
		})
	}

	if err := saveCustomPresets(custom); err != nil {
		return nil, err
	}
	return a.GetDNSPresets(), nil
}

// DeleteCustomDNSPreset deletes a user-defined DNS preset.
func (a *App) DeleteCustomDNSPreset(id string) ([]DNSPreset, error) {
	custom := loadCustomPresets()
	filtered := make([]DNSPreset, 0, len(custom))
	for _, c := range custom {
		if c.ID != id {
			filtered = append(filtered, c)
		}
	}
	if err := saveCustomPresets(filtered); err != nil {
		return nil, err
	}
	return a.GetDNSPresets(), nil
}

func encodePowerShell(cmd string) string {
	runes := utf16.Encode([]rune(cmd))
	b := make([]byte, len(runes)*2)
	for i, r := range runes {
		binary.LittleEndian.PutUint16(b[i*2:], r)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// SetAdapterDNS applies DNS settings to a specific adapter.
// Supports DHCP (automatic) or static primary & secondary DNS addresses with optional DoH template.
// Automatically requests UAC elevation if required.
func (a *App) SetAdapterDNS(alias string, isDHCP bool, primary string, secondary string, dohTemplate string) (string, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", errors.New("adapter alias is required")
	}

	dohTemplate = strings.TrimSpace(dohTemplate)
	if dohTemplate != "" && !strings.HasPrefix(strings.ToLower(dohTemplate), "https://") {
		dohTemplate = "https://" + dohTemplate
	}

	if !isDHCP {
		primary = strings.TrimSpace(primary)
		if primary == "" {
			return "", errors.New("primary DNS server is required")
		}
		if net.ParseIP(primary) == nil {
			return "", errors.New("invalid primary DNS IP address")
		}
		secondary = strings.TrimSpace(secondary)
		if secondary != "" && net.ParseIP(secondary) == nil {
			return "", errors.New("invalid secondary DNS IP address")
		}
	}

	// Try running netsh first (fast if already running with admin privileges)
	var commands [][]string
	if isDHCP {
		commands = append(commands, []string{"interface", "ipv4", "set", "dnsservers", fmt.Sprintf("name=%s", alias), "source=dhcp"})
	} else {
		commands = append(commands, []string{"interface", "ipv4", "set", "dnsservers", fmt.Sprintf("name=%s", alias), "source=static", fmt.Sprintf("address=%s", primary), "validate=no"})
		if secondary != "" {
			commands = append(commands, []string{"interface", "ipv4", "add", "dnsservers", fmt.Sprintf("name=%s", alias), fmt.Sprintf("address=%s", secondary), "index=2", "validate=no"})
		}
		if dohTemplate != "" {
			commands = append(commands, []string{"dns", "add", "encryption", fmt.Sprintf("server=%s", primary), fmt.Sprintf("dohtemplate=%s", dohTemplate), "autoupgrade=yes", "udpfallback=yes"})
			if secondary != "" {
				commands = append(commands, []string{"dns", "add", "encryption", fmt.Sprintf("server=%s", secondary), fmt.Sprintf("dohtemplate=%s", dohTemplate), "autoupgrade=yes", "udpfallback=yes"})
			}
		}
	}

	needsElevation := false
	for _, args := range commands {
		cmd := exec.Command("netsh", args...)
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			outStr := strings.ToLower(string(out) + " " + err.Error())
			if strings.Contains(outStr, "elevation") || strings.Contains(outStr, "administrator") || strings.Contains(outStr, "access is denied") {
				needsElevation = true
				break
			}
			if len(args) > 1 && args[0] == "dns" && args[1] == "add" {
				needsElevation = true
				break
			}
			return string(out), fmt.Errorf("%s (%w)", string(out), err)
		}
	}

	if !needsElevation {
		flushCmd := exec.Command("ipconfig", "/flushdns")
		hideWindow(flushCmd)
		_ = flushCmd.Run()
		return "DNS updated successfully.", nil
	}

	// Elevate via PowerShell Start-Process with Set-DnsClientServerAddress cmdlet and DoH registration
	var psInnerCmd string
	escapedAlias := strings.ReplaceAll(alias, "'", "''")
	if isDHCP {
		psInnerCmd = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceAlias '%s' -ResetServerAddresses; ipconfig /flushdns", escapedAlias)
	} else {
		var addrs []string
		addrs = append(addrs, fmt.Sprintf("'%s'", primary))
		if secondary != "" {
			addrs = append(addrs, fmt.Sprintf("'%s'", secondary))
		}

		dohScript := ""
		if dohTemplate != "" {
			dohScript = fmt.Sprintf(`function Set-DoH($server, $template) {
    if (-not $server -or -not $template) { return }
    try {
        if (Get-Command Set-DnsClientDohServerAddress -ErrorAction SilentlyContinue) {
            if (Get-DnsClientDohServerAddress -ServerAddress $server -ErrorAction SilentlyContinue) {
                Set-DnsClientDohServerAddress -ServerAddress $server -DohTemplate $template -AllowFallbackToUdp $true -AutoUpgrade $true -ErrorAction SilentlyContinue
            } else {
                Add-DnsClientDohServerAddress -ServerAddress $server -DohTemplate $template -AllowFallbackToUdp $true -AutoUpgrade $true -ErrorAction SilentlyContinue
            }
        } else {
            netsh dns add encryption server=$server dohtemplate=$template autoupgrade=yes udpfallback=yes
        }
    } catch {}
}; Set-DoH '%s' '%s';`, primary, dohTemplate)
			if secondary != "" {
				dohScript += fmt.Sprintf(` Set-DoH '%s' '%s';`, secondary, dohTemplate)
			}
		}

		psInnerCmd = fmt.Sprintf("%sSet-DnsClientServerAddress -InterfaceAlias '%s' -ServerAddresses @(%s); ipconfig /flushdns", dohScript, escapedAlias, strings.Join(addrs, ","))
	}

	encoded := encodePowerShell(psInnerCmd)
	elevCmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("Start-Process powershell.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-EncodedCommand','%s'", encoded))
	hideWindow(elevCmd)
	out, err := elevCmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("elevation failed: %s (%w)", string(out), err)
	}

	return "DNS updated successfully with administrator privileges.", nil
}

// RunIPConfig executes an ipconfig command (/renew, /release, /flushdns).
func (a *App) RunIPConfig(action string, adapter string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	adapter = strings.TrimSpace(adapter)

	var args []string
	switch action {
	case "renew":
		args = []string{"/renew"}
		if adapter != "" {
			args = append(args, adapter)
		}
	case "release":
		args = []string{"/release"}
		if adapter != "" {
			args = append(args, adapter)
		}
	case "flushdns":
		args = []string{"/flushdns"}
	default:
		return "", fmt.Errorf("unknown ipconfig action: %s", action)
	}

	cmd := exec.Command("ipconfig", args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		lower := strings.ToLower(outStr + " " + err.Error())
		if strings.Contains(lower, "elevation") || strings.Contains(lower, "administrator") || strings.Contains(lower, "access is denied") {
			psCmd := fmt.Sprintf("ipconfig %s", strings.Join(args, " "))
			encoded := encodePowerShell(psCmd)
			elevCmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
				fmt.Sprintf("Start-Process powershell.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-EncodedCommand','%s'", encoded))
			hideWindow(elevCmd)
			elevOut, elevErr := elevCmd.CombinedOutput()
			if elevErr != nil {
				return outStr, fmt.Errorf("elevation error: %s (%w)", string(elevOut), elevErr)
			}
			return fmt.Sprintf("Command 'ipconfig %s' executed successfully with elevated privileges.", strings.Join(args, " ")), nil
		}
		if outStr != "" {
			return outStr, fmt.Errorf("%s", outStr)
		}
		return "", err
	}

	return outStr, nil
}

// ------------------------------------------------------------- Ookla Speedtest CLI

// SpeedtestServer represents a nearby Ookla speedtest server.
type SpeedtestServer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Country  string `json:"country"`
	Host     string `json:"host"`
}

type speedtestServerListResponse struct {
	Type    string            `json:"type"`
	Servers []SpeedtestServer `json:"servers"`
}

func findSpeedtestBinary() string {
	if p, err := exec.LookPath("speedtest"); err == nil {
		return p
	}
	candidates := []string{
		`C:\ProgramData\chocolatey\bin\speedtest.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "speedtest", "speedtest.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Speedtest", "speedtest.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Speedtest", "speedtest.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// IsSpeedtestAvailable returns true if Ookla speedtest CLI binary exists.
func (a *App) IsSpeedtestAvailable() bool {
	return findSpeedtestBinary() != ""
}

// GetSpeedtestServers fetches the nearest servers from Ookla speedtest CLI.
func (a *App) GetSpeedtestServers() ([]SpeedtestServer, error) {
	bin := findSpeedtestBinary()
	if bin == "" {
		return nil, errors.New("speedtest CLI not found")
	}

	cmd := exec.Command(bin, "--accept-license", "--accept-gdpr", "-L", "-f", "json")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch servers: %s (%w)", string(out), err)
	}

	var resp speedtestServerListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse servers: %w", err)
	}
	return resp.Servers, nil
}

// fastTarget holds one Netflix OCA speedtest target endpoint.
type fastTarget struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Location struct {
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"location"`
}

type fastMeta struct {
	Client struct {
		IP  string `json:"ip"`
		ISP string `json:"isp"`
		ASN string `json:"asn"`
	} `json:"client"`
	Targets []fastTarget `json:"targets"`
}

func getFastToken(client *http.Client) string {
	defaultToken := "YXNkZmFzZGxmbnNkYWZoYXNkZmhrYWxm"
	resp, err := client.Get("https://fast.com")
	if err != nil {
		return defaultToken
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	reScript := regexp.MustCompile(`/app-[a-zA-Z0-9]+\.js`)
	scriptPath := reScript.FindString(string(body))
	if scriptPath == "" {
		return defaultToken
	}

	jsResp, err := client.Get("https://fast.com" + scriptPath)
	if err != nil {
		return defaultToken
	}
	defer jsResp.Body.Close()
	jsBody, _ := io.ReadAll(jsResp.Body)

	reToken := regexp.MustCompile(`token[:=]\s*["']([^"']+)["']`)
	matches := reToken.FindAllStringSubmatch(string(jsBody), -1)
	for _, m := range matches {
		if len(m) > 1 && len(m[1]) > 8 {
			return m[1]
		}
	}
	return defaultToken
}

func fetchFastMeta(ctx context.Context, client *http.Client, token string) (*fastMeta, error) {
	apiURL := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=5", token)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Fast.com API responded with status %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var meta fastMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse Fast.com API response: %w", err)
	}
	return &meta, nil
}

func (a *App) createBoundHTTPClient(targetIfaceOrIP string) *http.Client {
	targetIfaceOrIP = strings.TrimSpace(targetIfaceOrIP)
	var localIP net.IP
	if targetIfaceOrIP != "" {
		if ip := net.ParseIP(targetIfaceOrIP); ip != nil {
			localIP = ip
		} else if adapters, err := a.GetAdapters(); err == nil {
			for _, ad := range adapters {
				if strings.EqualFold(ad.Alias, targetIfaceOrIP) && len(ad.IPv4) > 0 {
					localIP = net.ParseIP(ad.IPv4[0])
					break
				}
			}
		}
	}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if localIP != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: localIP}
	}

	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
	}
}

// GetFastServers fetches the nearest Netflix OCA servers from Fast.com API.
func (a *App) GetFastServers() ([]SpeedtestServer, error) {
	client := a.createBoundHTTPClient("")
	token := getFastToken(client)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	meta, err := fetchFastMeta(ctx, client, token)
	if err != nil {
		return nil, err
	}

	var list []SpeedtestServer
	for i, t := range meta.Targets {
		city := t.Location.City
		if city == "" {
			city = t.Name
		}
		list = append(list, SpeedtestServer{
			ID:       i + 1,
			Name:     fmt.Sprintf("Netflix OCA (%s)", city),
			Location: city,
			Country:  t.Location.Country,
			Host:     t.URL,
		})
	}
	return list, nil
}

// StartSpeedtest starts a Speedtest run (supporting "fast" or "ookla") and streams events to the frontend.
func (a *App) StartSpeedtest(engine string, serverID int, targetIfaceOrIP string) error {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		if a.IsSpeedtestAvailable() {
			engine = "ookla"
		} else {
			engine = "fast"
		}
	}

	if engine == "ookla" {
		bin := findSpeedtestBinary()
		if bin == "" {
			return errors.New("Speedtest by Ookla CLI not found. Please install it via Chocolatey ('choco install speedtest') or select Fast.com.")
		}

		a.speedtestMu.Lock()
		if a.speedtestCancel != nil {
			a.speedtestMu.Unlock()
			return errors.New("a speedtest is already in progress")
		}
		ctx, cancel := context.WithCancel(a.ctx)
		a.speedtestCancel = cancel
		a.speedtestMu.Unlock()

		go a.runSpeedtest(ctx, bin, serverID, targetIfaceOrIP)
		return nil
	}

	// Default to Fast.com
	a.speedtestMu.Lock()
	if a.speedtestCancel != nil {
		a.speedtestMu.Unlock()
		return errors.New("a speedtest is already in progress")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.speedtestCancel = cancel
	a.speedtestMu.Unlock()

	go a.runFastSpeedtest(ctx, serverID, targetIfaceOrIP)
	return nil
}

func (a *App) runFastSpeedtest(ctx context.Context, serverID int, targetIfaceOrIP string) {
	defer func() {
		a.speedtestMu.Lock()
		a.speedtestCancel = nil
		a.speedtestMu.Unlock()
	}()

	wr.EventsEmit(a.ctx, "speedtest:raw", "> Connecting to Fast.com (Netflix Open Connect)…")

	client := a.createBoundHTTPClient(targetIfaceOrIP)

	token := getFastToken(client)
	wr.EventsEmit(a.ctx, "speedtest:raw", "> Authenticated with Fast.com API")

	if ctx.Err() != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"cancelled": true})
		return
	}

	meta, err := fetchFastMeta(ctx, client, token)
	if err != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{
			"error": fmt.Sprintf("Failed to contact Fast.com API: %v", err),
		})
		return
	}

	if len(meta.Targets) == 0 {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{
			"error": "No Fast.com test servers returned from API",
		})
		return
	}

	selectedTarget := meta.Targets[0]
	if serverID > 0 && serverID <= len(meta.Targets) {
		selectedTarget = meta.Targets[serverID-1]
	}

	serverName := fmt.Sprintf("Netflix OCA (%s)", selectedTarget.Location.City)
	serverLoc := fmt.Sprintf("%s, %s", selectedTarget.Location.City, selectedTarget.Location.Country)
	wr.EventsEmit(a.ctx, "speedtest:raw", fmt.Sprintf("> Connected to %s [%s]", serverName, serverLoc))
	wr.EventsEmit(a.ctx, "speedtest:raw", fmt.Sprintf("> Client IP: %s (ISP: %s)", meta.Client.IP, meta.Client.ISP))

	wr.EventsEmit(a.ctx, "speedtest:event", map[string]interface{}{
		"type": "testStart",
		"isp":  meta.Client.ISP,
		"server": map[string]interface{}{
			"name":     serverName,
			"location": serverLoc,
		},
	})

	// Latency test
	wr.EventsEmit(a.ctx, "speedtest:raw", "> Measuring unloaded latency & jitter…")
	avgLat, jitter := measureFastLatency(ctx, client, selectedTarget.URL)
	wr.EventsEmit(a.ctx, "speedtest:event", map[string]interface{}{
		"type": "ping",
		"ping": map[string]interface{}{
			"latency": avgLat,
			"jitter":  jitter,
		},
	})
	wr.EventsEmit(a.ctx, "speedtest:raw", fmt.Sprintf("> Latency: %.1f ms | Jitter: %.1f ms", avgLat, jitter))

	if ctx.Err() != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"cancelled": true})
		return
	}

	// Download test
	wr.EventsEmit(a.ctx, "speedtest:raw", "> Starting download test…")
	dlBps, err := a.measureFastDownload(ctx, client, meta.Targets, selectedTarget, serverID)
	if err != nil && ctx.Err() != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"cancelled": true})
		return
	}
	wr.EventsEmit(a.ctx, "speedtest:raw", fmt.Sprintf("> Download speed: %.2f Mbps", (float64(dlBps)*8)/1_000_000))

	if ctx.Err() != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"cancelled": true})
		return
	}

	// Upload test
	wr.EventsEmit(a.ctx, "speedtest:raw", "> Starting upload test…")
	upBps, err := a.measureFastUpload(ctx, client, meta.Targets, selectedTarget, serverID)
	if err != nil && ctx.Err() != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"cancelled": true})
		return
	}
	wr.EventsEmit(a.ctx, "speedtest:raw", fmt.Sprintf("> Upload speed: %.2f Mbps", (float64(upBps)*8)/1_000_000))

	// Final result
	wr.EventsEmit(a.ctx, "speedtest:event", map[string]interface{}{
		"type": "result",
		"download": map[string]interface{}{
			"bandwidth": dlBps,
		},
		"upload": map[string]interface{}{
			"bandwidth": upBps,
		},
		"ping": map[string]interface{}{
			"latency": avgLat,
			"jitter":  jitter,
		},
		"packetLoss": 0.0,
		"isp":        meta.Client.ISP,
		"server": map[string]interface{}{
			"name":     serverName,
			"location": serverLoc,
		},
		"result": map[string]interface{}{
			"url": "https://fast.com",
		},
	})

	wr.EventsEmit(a.ctx, "speedtest:raw", fmt.Sprintf("> Speedtest completed! Download: %.2f Mbps, Upload: %.2f Mbps, Ping: %.1f ms",
		(float64(dlBps)*8)/1_000_000, (float64(upBps)*8)/1_000_000, avgLat))

	wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{
		"cancelled": false,
	})
}

func measureFastLatency(ctx context.Context, client *http.Client, targetURL string) (float64, float64) {
	var rtts []float64
	for i := 0; i < 4; i++ {
		if ctx.Err() != nil {
			break
		}
		t0 := time.Now()
		req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Range", "bytes=0-0")
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			rtts = append(rtts, float64(time.Since(t0).Microseconds())/1000.0)
		}
		time.Sleep(40 * time.Millisecond)
	}

	if len(rtts) == 0 {
		return 0, 0
	}

	sum := 0.0
	for _, v := range rtts {
		sum += v
	}
	avgLat := sum / float64(len(rtts))

	diffSum := 0.0
	for i := 1; i < len(rtts); i++ {
		diffSum += math.Abs(rtts[i] - rtts[i-1])
	}
	jitter := 0.0
	if len(rtts) > 1 {
		jitter = diffSum / float64(len(rtts)-1)
	}

	return avgLat, jitter
}

func (a *App) measureFastDownload(ctx context.Context, client *http.Client, targets []fastTarget, selected fastTarget, serverID int) (int64, error) {
	dlDuration := 7 * time.Second
	dlCtx, dlCancel := context.WithTimeout(ctx, dlDuration)
	defer dlCancel()

	var urls []string
	if serverID > 0 {
		urls = []string{strings.Replace(selected.URL, "/speedtest?", "/speedtest/range/0-25000000?", 1)}
	} else {
		for _, t := range targets {
			urls = append(urls, strings.Replace(t.URL, "/speedtest?", "/speedtest/range/0-25000000?", 1))
		}
	}
	if len(urls) == 0 {
		urls = []string{strings.Replace(selected.URL, "/speedtest?", "/speedtest/range/0-25000000?", 1)}
	}

	var totalBytes int64
	var wg sync.WaitGroup
	start := time.Now()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-dlCtx.Done():
				return
			case t := <-ticker.C:
				elapsed := t.Sub(start).Seconds()
				if elapsed > 0 {
					b := atomic.LoadInt64(&totalBytes)
					bps := float64(b) / elapsed
					progress := math.Min(1.0, elapsed/dlDuration.Seconds())
					wr.EventsEmit(a.ctx, "speedtest:event", map[string]interface{}{
						"type": "download",
						"download": map[string]interface{}{
							"bandwidth": int64(bps),
							"progress":  progress,
						},
					})
				}
			}
		}
	}()

	numWorkers := 3
	if len(urls) > 3 {
		numWorkers = 4
	}
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		targetURL := urls[i%len(urls)]
		go func(url string) {
			defer wg.Done()
			buf := make([]byte, 32768)
			for dlCtx.Err() == nil {
				req, err := http.NewRequestWithContext(dlCtx, "GET", url, nil)
				if err != nil {
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					return
				}
				for {
					n, err := resp.Body.Read(buf)
					if n > 0 {
						atomic.AddInt64(&totalBytes, int64(n))
					}
					if err != nil {
						_ = resp.Body.Close()
						break
					}
				}
			}
		}(targetURL)
	}

	wg.Wait()
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	return int64(float64(totalBytes) / elapsed), nil
}

func (a *App) measureFastUpload(ctx context.Context, client *http.Client, targets []fastTarget, selected fastTarget, serverID int) (int64, error) {
	upDuration := 5 * time.Second
	upCtx, upCancel := context.WithTimeout(ctx, upDuration)
	defer upCancel()

	var urls []string
	if serverID > 0 {
		urls = []string{selected.URL}
	} else {
		for _, t := range targets {
			urls = append(urls, t.URL)
		}
	}
	if len(urls) == 0 {
		urls = []string{selected.URL}
	}

	var totalBytes int64
	var wg sync.WaitGroup
	start := time.Now()

	dummyChunk := make([]byte, 1024*1024) // 1MB
	_, _ = rand.Read(dummyChunk)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-upCtx.Done():
				return
			case t := <-ticker.C:
				elapsed := t.Sub(start).Seconds()
				if elapsed > 0 {
					b := atomic.LoadInt64(&totalBytes)
					bps := float64(b) / elapsed
					progress := math.Min(1.0, elapsed/upDuration.Seconds())
					wr.EventsEmit(a.ctx, "speedtest:event", map[string]interface{}{
						"type": "upload",
						"upload": map[string]interface{}{
							"bandwidth": int64(bps),
							"progress":  progress,
						},
					})
				}
			}
		}
	}()

	numWorkers := 2
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		targetURL := urls[i%len(urls)]
		go func(url string) {
			defer wg.Done()
			for upCtx.Err() == nil {
				req, err := http.NewRequestWithContext(upCtx, "POST", url, bytes.NewReader(dummyChunk))
				if err != nil {
					return
				}
				req.Header.Set("Content-Type", "application/octet-stream")
				resp, err := client.Do(req)
				if err == nil {
					atomic.AddInt64(&totalBytes, int64(len(dummyChunk)))
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}(targetURL)
	}

	wg.Wait()
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}
	return int64(float64(totalBytes) / elapsed), nil
}

func (a *App) runSpeedtest(ctx context.Context, bin string, serverID int, targetIfaceOrIP string) {
	defer func() {
		a.speedtestMu.Lock()
		a.speedtestCancel = nil
		a.speedtestCmd = nil
		a.speedtestMu.Unlock()
	}()

	args := []string{"--accept-license", "--accept-gdpr", "-f", "jsonl", "-p", "yes"}
	if serverID > 0 {
		args = append(args, "-s", strconv.Itoa(serverID))
	}

	targetIfaceOrIP = strings.TrimSpace(targetIfaceOrIP)
	if targetIfaceOrIP != "" {
		ip := targetIfaceOrIP
		if net.ParseIP(ip) == nil {
			// Try to find the adapter's primary IPv4 address
			if adapters, err := a.GetAdapters(); err == nil {
				for _, ad := range adapters {
					if strings.EqualFold(ad.Alias, targetIfaceOrIP) && len(ad.IPv4) > 0 {
						ip = ad.IPv4[0]
						break
					}
				}
			}
		}
		if net.ParseIP(ip) != nil {
			args = append(args, "-i", ip)
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	hideWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"error": err.Error()})
		return
	}

	if err := cmd.Start(); err != nil {
		wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{"error": err.Error()})
		return
	}

	a.speedtestMu.Lock()
	a.speedtestCmd = cmd
	a.speedtestMu.Unlock()

	doneChan := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
				hideWindow(killCmd)
				_ = killCmd.Run()
			}
			_ = stdout.Close()
		case <-doneChan:
		}
	}()

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}

		wr.EventsEmit(a.ctx, "speedtest:raw", line)

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			wr.EventsEmit(a.ctx, "speedtest:event", ev)
		}
	}

	close(doneChan)
	_ = cmd.Wait()

	cancelled := ctx.Err() != nil
	wr.EventsEmit(a.ctx, "speedtest:done", map[string]interface{}{
		"cancelled": cancelled,
	})
}

// StopSpeedtest terminates an ongoing speedtest immediately.
func (a *App) StopSpeedtest() {
	a.speedtestMu.Lock()
	defer a.speedtestMu.Unlock()

	if a.speedtestCancel != nil {
		a.speedtestCancel()
	}

	if a.speedtestCmd != nil && a.speedtestCmd.Process != nil {
		pid := a.speedtestCmd.Process.Pid
		_ = a.speedtestCmd.Process.Kill()
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		hideWindow(killCmd)
		_ = killCmd.Run()
	}
}



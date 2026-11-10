<div align="center">

<img src="assets/icon.png" alt="NetPulse Logo" width="128" height="128" style="border-radius: 28px; box-shadow: 0 12px 36px rgba(124, 140, 255, 0.45);" />

# NetPulse
### Modern Network Telemetry, Diagnostics & Dual-Engine Benchmark Suite for Windows

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org)
[![Wails v2](https://img.shields.io/badge/Wails-v2.16-df0000?style=for-the-badge&logo=wails&logoColor=white)](https://wails.io)
[![Platform](https://img.shields.io/badge/Platform-Windows%2010%20%7C%2011-0078D6?style=for-the-badge&logo=windows&logoColor=white)](https://microsoft.com/windows)
[![AI Assisted](https://img.shields.io/badge/AI%20Code%20Assisted-Gemini%20%7C%20DeepMind-8A2BE2?style=for-the-badge&logo=google&logoColor=white)](https://deepmind.google)
[![License](https://img.shields.io/badge/License-MIT-4ade80?style=for-the-badge)](LICENSE)

*A blazing-fast, frameless desktop network command center engineered with Go and Wails v2. Monitor adapters, inspect Wi-Fi 4–7 channel width telemetry, run live latency sparklines, benchmark with both **Fast.com (Netflix OCA)** and the official **Ookla Speedtest® CLI**, manage encrypted DNS & DoH presets, execute ipconfig tools, and access router portals with automated credentials.*

---

</div>

## 🌟 Key Features

### ⚡ Multi-Interface & Wi-Fi 7 Telemetry
- **Active Adapter Enumeration**: Instant detection of all active Ethernet, Wi-Fi, and virtual interfaces with operational state indicators.
- **Full Address Inspection**: Comprehensive listing of IPv4, IPv6, Default Gateways, DNS servers, and physical MAC addresses.
- **Deep Wi-Fi 4–7 Analytics**:
  - Live SSID and 802.11 generation identification (Wi-Fi 4, 5, 6, 6E, and **Wi-Fi 7** / 802.11be).
  - **Channel Width Detection**: Live extraction of operational channel bandwidths (**20 MHz**, **40 MHz**, **80 MHz**, **160 MHz**, and **320 MHz**) parsed directly from Windows Native Wifi raw beacon Information Elements (HT, VHT, HE, and EHT).
  - Radio band (2.4 GHz, 5 GHz, 6 GHz), channel allocation, signal quality percentage with visual bars, and Rx/Tx link rates.
- **1-Click Copy**: Click any IP address, MAC, gateway, or DNS server badge to immediately copy it to your clipboard.

### 🚀 Dual-Engine Speedtest Suite
NetPulse features a built-in provider selector allowing side-by-side benchmarking:
- **Fast.com (Netflix Open Connect)**:
  - **Zero External Dependencies**: Pure Go implementation that requires no third-party CLI or binary installation.
  - Benchmarks video streaming throughput directly via nearby Netflix Open Connect Appliances (OCAs).
  - Measures unloaded ping latency, jitter, multi-stream download throughput, and HTTP POST upload bandwidth.
  - Server auto-selection or manual targeting of specific regional OCA nodes (e.g., Manila, Quezon City, Makati).
- **Speedtest by Ookla® CLI**:
  - Native integration with the official Ookla CLI via structured JSONL streams.
  - Discovers nearby optimal Ookla test nodes with human-readable sponsor and location metadata.
  - Detailed telemetry: latency, jitter, packet loss percentage, ISP, and permanent Speedtest.net result links.
- **Per-Adapter Route Binding**: Bind speedtests to specific network interfaces or IP addresses to test individual NICs.
- **Real-Time Visuals**: Dual animated progress bars, live Mbps meters, and clean background process cancellation.

### 📶 Real-Time Ping Telemetry & Sparkline Graph
- **Interactive Target Switching**: Pre-configured quick chips for Default Gateway, Cloudflare (`1.1.1.1`), Google DNS (`8.8.8.8`), and `google.com`, with custom host/IP input.
- **Burst or Continuous Mode**: Selectable sample counts (4, 10, 25, or continuous `∞`).
- **Dynamic SVG Sparkline**: Live historical latency graph updating on every ICMP echo reply.
- **Tabular Performance Stats**: Instant calculation of Sent, Received, Loss %, Min, Avg, and Max latency.
- **Instant Health Verdict**: Automatic qualitative rating (*Excellent*, *Fair*, *Slow*, *Packet lost*).

### 🛡️ DNS Presets & DNS-over-HTTPS (DoH) Manager
- **Curated 1-Click Presets**:
  - **Cloudflare** (`1.1.1.1` / `1.0.0.1` + DoH)
  - **Google Public DNS** (`8.8.8.8` / `8.8.4.4` + DoH)
  - **Quad9** (`9.9.9.9` / `149.112.112.112` + DoH)
  - **AdGuard DNS** (Ad & tracker blocking + DoH)
  - **OpenDNS** (`208.67.222.222` / `208.67.220.220` + DoH)
  - **NextDNS** (Custom cloud DNS profiles + DoH)
  - **Automatic (DHCP)**: One-click revert to router-assigned DNS.
- **Custom Preset Builder**: Save your own custom primary/secondary DNS servers and encrypted DoH template URLs.
- **Automatic UAC Elevation**: Applies network configuration via Windows `netsh` subsystem, prompting for administrative elevation seamlessly if required.

### 🛠️ Windows IP Toolkit (`ipconfig`)
- **`ipconfig /renew`**: Re-negotiate DHCP lease for a specific interface or all adapters.
- **`ipconfig /release`**: Release DHCP IP leases immediately.
- **`ipconfig /flushdns`**: Purge the Windows DNS resolver cache.
- **Interactive Terminal**: Dedicated dark command console displaying raw execution output with clear controls.

### 🌐 Router Portal & Multi-Location Manager
- **Location Selector**: Manage multiple router profiles (e.g., Home, Office) with custom URLs, usernames, and passwords.
- **User Documents Storage**: Preserves profiles in `%USERPROFILE%\Documents\NetworkInspector\pldt_locations.json`.
- **Default Browser Launch**: Launches router portals directly in your system default web browser, automatically copying router credentials to the clipboard.
- **JavaScript Auto-Login Helper**: Generates and copies ready-to-use DOM automation scripts for modem portals.

---

## 🤖 AI Code Assistance

This project was developed with the assistance of **AI pair programming** powered by **Google DeepMind Antigravity / Gemini**. 

AI assisted with:
- Designing and implementing low-level Win32 Native Wifi (`wlanapi.dll`) beacon Information Element (IE) decoders for Wi-Fi 6/7 channel width extraction.
- Engineering the native pure-Go Fast.com benchmark engine (Netflix OCA token handshake, chunked range downloading, and streaming POST uploads).
- Crafting modern glassmorphic UI components, real-time SVG sparkline charts, and responsive layouts.
- Optimizing concurrency pipelines, graceful cancellation handlers, and process watchdog safeguards.

---

## 🏗️ Architecture & Technology Stack

```
┌────────────────────────────────────────────────────────┐
│                   NetPulse (Desktop UI)                │
│  Vanilla ES6+ • CSS Glassmorphism • SVG Sparklines     │
└──────────────────────────┬─────────────────────────────┘
                           │ Wails v2 RPC Bridge
┌──────────────────────────▼─────────────────────────────┐
│                 Go Application Backend                 │
│  app.go • Concurrency Watchdogs • Native Fast.com API  │
└────────────┬─────────────┬─────────────┬───────────────┘
             │             │             │
   Win32 / IP Helper      netsh / WMI    Ookla CLI / Fast.com
   (Adapter & Wi-Fi)    (DNS & IPConfig)  (Dual Benchmark Suite)
```

| Layer | Technology | Details |
|---|---|---|
| **Backend** | Go 1.21+ | High-concurrency runtime, Native Wifi IE parser, socket binding |
| **Framework** | Wails v2 | Ultra-lean desktop wrapper using native Edge WebView2 (no Electron bloat) |
| **Frontend** | Vanilla ES6+ & CSS | Modern glassmorphism, zero npm build step, pure CSS backdrop filters |
| **Networking** | Win32 & netsh | Windows IP helper API, `wlanapi.dll`, `netsh`, ICMP echo wrappers |
| **Benchmark** | Fast.com + Ookla | Pure Go Fast.com engine + optional Ookla CLI integration |

---

## 📦 Getting Started

### Prerequisites
1. **Windows 10 / 11** (64-bit).
2. **Microsoft Edge WebView2 Runtime** (pre-installed on Windows 10/11).
3. **Go 1.21 or newer** ([Download Go](https://go.dev/dl/)).
4. **Wails v2 CLI**:
   ```powershell
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```
5. *(Optional)* **Speedtest by Ookla CLI** (only required if you wish to use Ookla instead of the built-in Fast.com engine):
   ```powershell
   choco install speedtest
   # or via winget:
   winget install Ookla.Speedtest.CLI
   ```

---

## 🔨 Building from Source

Clone the repository and build using optimized production flags:

```powershell
# 1. Clone the repository
git clone https://github.com/XxeylakxX/netinspector.git
cd netinspector

# 2. Tidy Go dependencies
go mod tidy

# 3. Development Mode (Live reload)
wails dev

# 4. Production Clean Build
wails build -clean -trimpath -ldflags="-s -w"
```

The compiled standalone executable will be located at:
```
build\bin\NetworkInspector.exe
```

---

## 🚀 GitHub Actions Manual Workflow

This repository includes a manual build and release pipeline configured at [`.github/workflows/build.yml`](.github/workflows/build.yml).

### How to trigger a build manually:
1. Navigate to the **Actions** tab in the GitHub repository.
2. Select the **Build & Package Windows Executable** workflow from the left sidebar.
3. Click the **Run workflow** dropdown on the right.
4. *(Optional)* Check **Publish release asset** and enter a version tag (e.g. `v1.0.0`) to automatically generate a GitHub Release.
5. Click **Run workflow**. Once finished, the compiled `NetworkInspector.exe` will be available in the workflow **Artifacts** section!

---

## ⌨️ Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| <kbd>F5</kbd> | Refresh all network adapters, channel width, and Wi-Fi metrics |
| <kbd>Enter</kbd> | Start / Stop Ping check (when host input is focused) |
| <kbd>Esc</kbd> | Close any open modal (DNS, Router Portal) |
| <kbd>Ctrl</kbd> + <kbd>Q</kbd> | Quit application |
| **Double-Click Titlebar** | Maximize / Restore window |

---

## 📂 Configuration Storage

All persistent data is stored inside the user's personal documents directory:

```
%USERPROFILE%\Documents\NetworkInspector\
└── pldt_locations.json
```

---

## 📄 License

Distributed under the **MIT License**. See [`LICENSE`](LICENSE) for more information.

<div align="center">
<sub>Crafted with Go, Wails v2, and passion for high-performance Windows network telemetry.</sub>
</div>

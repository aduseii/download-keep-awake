<p align="center"><img src="assets/icon.png" width="96" alt=""></p>

<h1 align="center">Download Keep-Awake</h1>

<p align="center">A tiny Windows 11 / 10 tray app that stops your PC from sleeping or hibernating <b>while anything is downloading</b> — and gives your normal sleep settings back the moment it's done.</p>

---

## Why

Windows' sleep timer doesn't know you're halfway through a 20 GB download. Turning sleep off entirely means your PC stays on all night after the download finishes. Download Keep-Awake sits in the system tray and blocks sleep **only while data is actually downloading** — from any app.

## What it detects

The PC stays awake while **any** of these sees a download (each can be switched on/off from the tray menu):

| Detector | Catches | How |
|---|---|---|
| **Any download (network traffic)** | Everything: browsers, Steam / Epic / Xbox / Battle.net, torrents, Windows Update, cloud sync, download managers | Incoming traffic on your real network adapters (Wi-Fi, Ethernet, mobile) above **150 KB/s**, averaged over 15 s |
| **Browser & torrent downloads** | Even slow downloads in Chrome, Edge, Brave, Opera, Firefox, qBittorrent, uTorrent, BitComet… | In-progress files that are still growing in your Downloads folder (`.crdownload`, `.part`, `.!qB`, `.!ut`…) |
| **Download manager apps** | IDM, Free Download Manager, Neat DM, JDownloader, XDM, Motrix, EagleGet, uGet, FlashGet, aria2, Persepolis | The app's own data activity above 64 KB/s |

Only **downloading** counts — uploading or torrent seeding doesn't keep your PC awake. VPN, virtual-machine and loopback adapters are ignored so traffic isn't counted twice.

## Features

- **Automatic** – nothing to switch on or off; just leave it running.
- **Stall-tolerant** – waits 90 seconds (configurable) after a download stops before allowing sleep, so a short stall or a gap between files doesn't end your download.
- **Leaves your screen alone** – the display can still turn off as normal (optional setting to keep it on).
- **Tray icon shows the state at a glance**
  - 🟢 Green – keeping the PC awake for a download
  - 🔵 Blue – kept awake manually ("Keep awake now")
  - ⚪ Grey – idle, normal sleep allowed
  - 🟠 Amber – paused
- **Live status** in the tray menu: current download speed and what was detected.
- **Notifications** when a download starts keeping the PC awake and when sleep is allowed again.
- **Updates itself** – checks GitHub for new versions and installs them automatically (never in the middle of a download).
- **Start with Windows**, **Pause** and **Keep awake now** in the tray menu.
- **Single portable `.exe`**, ~6 MB, no installer, no admin rights, no .NET or other runtime needed.

> **Streaming counts as downloading.** HD video streams above the 150 KB/s threshold keep the PC awake too (video players usually do that anyway). If you'd rather they didn't, raise `networkMinSpeedKBps` or switch off "Detect any download" and rely on the other two detectors.

## Install

1. Download **`DownloadKeepAwake.exe`** from the [latest release](../../releases/latest).
   (On a Snapdragon / ARM PC, use `DownloadKeepAwake-arm64.exe`.)
2. Put it somewhere permanent, e.g. `C:\Users\<you>\Apps\DownloadKeepAwake.exe`.
3. Double-click it. On first run it asks whether to start with Windows.

> **"Windows protected your PC"?** The app isn't code-signed yet, so SmartScreen may warn the first time. Click **More info → Run anyway**.

The round icon appears in the system tray (click the **^** next to the clock if it's hidden; you can drag it onto the taskbar to keep it visible).

## Settings

Right-click the tray icon → **Settings…** opens `%APPDATA%\DownloadKeepAwake\config.json` in Notepad. Save the file and changes apply within a few seconds — no restart needed. The three detectors can also be toggled straight from the tray menu.

| Setting | What it does | Default |
|---|---|---|
| `detectNetwork` | Detect any download from any app via network traffic. | `true` |
| `networkMinSpeedKBps` | Incoming speed that counts as downloading. | `150` |
| `networkWindowSeconds` | Speed is averaged over this many seconds, so brief bursts are ignored. | `15` |
| `detectPartialFiles` | Detect in-progress download files that are still growing. | `true` |
| `watchFolders` | Folders to watch (and two levels of subfolders). `%DOWNLOADS%` = your Downloads folder, wherever it is. Add your torrent or Steam download folders here if they're elsewhere. | `["%DOWNLOADS%"]` |
| `partialExtensions` | File endings that mean "still downloading". | `.crdownload`, `.part`, `.!qB`, … |
| `detectApps` | Detect download manager programs. | `true` |
| `downloadManagers` | Programs to watch. Find the name in **Task Manager → Details** (`.exe` is optional). | see table above |
| `minSpeedKBps` | Activity from those programs that counts as downloading. | `64` |
| `graceSeconds` | How long to stay awake after a download stops or stalls. | `90` |
| `checkSeconds` | How often to check. | `3` |
| `keepScreenOn` | Also stop the display from turning off while downloading. | `false` |
| `notifications` | Show a notification when keep-awake starts and stops. | `true` |
| `checkForUpdates` | Look for a new version on GitHub 30 s after start and every 6 hours. | `true` |
| `autoInstallUpdates` | Install new versions automatically. When off, you get a notification and install from the tray menu. | `true` |

Upgrading from an older version? Your existing settings are kept and the new options are added automatically.

## How it works

Every few seconds the app reads Windows' network adapter counters (`GetIfTable2`), scans your Downloads folder for growing partial files, and reads the I/O counters of known download managers. If any of them shows a download, it tells Windows the system is in use via [`SetThreadExecutionState`](https://learn.microsoft.com/windows/win32/api/winbase/nf-winbase-setthreadexecutionstate) (`ES_SYSTEM_REQUIRED`) — the same mechanism video players and installers use. When nothing has been downloading for `graceSeconds`, it releases that request and Windows' own sleep timer takes over again.

It prevents **automatic idle** sleep and hibernation. It deliberately does *not* override you: choosing Sleep from the Start menu, closing a laptop lid, or critical-battery hibernation still work as usual. You can see it working with `powercfg /requests` in an admin terminal.

## Automatic updates

The app keeps itself up to date from this repository's [Releases](../../releases):

1. 30 seconds after starting, and every 6 hours after that, it asks GitHub for the latest release.
2. If there's a newer version, it downloads the `.exe` for your PC (x64 or ARM64) **next to the current one**.
3. The download is checked against `SHA256SUMS.txt` from the same release. Anything that doesn't match is thrown away.
4. As soon as nothing is downloading, it swaps the new `.exe` in, restarts itself in about a second and shows an **"Updated to vX.Y.Z"** notification. If something is downloading, it waits until the download is done.

You can also use **Check for updates** in the tray menu, or untick **Install updates automatically** to be asked first.

For updates to work, keep the `.exe` in a folder you can write to (e.g. `Documents`, `%LOCALAPPDATA%\Programs`), not in `C:\Program Files`. The repository must be public so the app can see its releases.

## Uninstall

Right-click the tray icon → untick **Start with Windows** → **Exit**, then delete the `.exe` and the `%APPDATA%\DownloadKeepAwake` folder.

## Building from source

Requires [Go](https://go.dev/dl/) 1.22+. No other dependencies — it talks to the Windows API directly.

```sh
# Windows
go build -trimpath -ldflags "-s -w -H windowsgui" -o DownloadKeepAwake.exe .

# Cross-compile from Linux / macOS
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o DownloadKeepAwake.exe .
```

The icon and manifest are embedded from the committed `rsrc_windows_*.syso` files. If you change `assets/icon.ico` or `assets/app.manifest`, regenerate them with `go install github.com/akavel/rsrc@latest` then `go generate`.

### Releasing

Push a version tag and GitHub Actions builds both executables, publishes a release, and **every installed copy updates itself within 6 hours**:

```sh
git tag v1.0.0
git push origin v1.0.0
```

## License

[MIT](LICENSE)

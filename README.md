<p align="center"><img src="assets/icon.png" width="96" alt=""></p>

<h1 align="center">Download Keep-Awake</h1>

<p align="center">A tiny Windows 11 / 10 tray app that stops your PC from sleeping or hibernating <b>while your download manager is downloading</b> — and gives your normal sleep settings back the moment it's done.</p>

---

## Why

Windows' sleep timer doesn't know you're halfway through a 20 GB download. Turning sleep off entirely means your PC stays on all night after the download finishes. Download Keep-Awake sits in the system tray, watches your download manager, and blocks sleep **only while data is actually flowing**.

## Features

- **Automatic** – detects real download activity from your download manager; nothing to switch on or off.
- **Precise** – measures only the download manager's own traffic, so YouTube, games or Windows Update don't keep your PC awake.
- **Stall-tolerant** – waits 90 seconds (configurable) after a download stops before allowing sleep, so a short stall or a gap between files doesn't end your download.
- **Leaves your screen alone** – the display can still turn off as normal (optional setting to keep it on).
- **Tray icon shows the state at a glance**
  - 🟢 Green – keeping the PC awake for a download
  - 🔵 Blue – kept awake manually ("Keep awake now")
  - ⚪ Grey – idle, normal sleep allowed
  - 🟠 Amber – paused
- **Notifications** when a download starts keeping the PC awake and when sleep is allowed again.
- **Start with Windows** toggle, **Pause** and **Keep awake now** in the tray menu.
- **Single portable `.exe`**, ~2 MB, no installer, no admin rights, no .NET or other runtime needed.

## Supported download managers

Works out of the box with:

| Download manager | Process name |
|---|---|
| Free Download Manager | `fdm` |
| Internet Download Manager (IDM) | `IDMan` |
| Neat Download Manager | `NeatDM` |
| Xtreme Download Manager | `xdm-app`, `xdman` |
| JDownloader 2 | `JDownloader2` |
| Motrix | `Motrix` |
| EagleGet | `EagleGet` |
| uGet | `uget-gtk` |
| FlashGet | `FlashGet` |
| aria2 | `aria2c` |

Using something else (or a torrent client)? Add it in **Settings** — see below.

## Install

1. Download **`DownloadKeepAwake.exe`** from the [latest release](../../releases/latest).
   (On a Snapdragon / ARM PC, use `DownloadKeepAwake-arm64.exe`.)
2. Put it somewhere permanent, e.g. `C:\Users\<you>\Apps\DownloadKeepAwake.exe`.
3. Double-click it. On first run it asks whether to start with Windows.

> **"Windows protected your PC"?** The app isn't code-signed yet, so SmartScreen may warn the first time. Click **More info → Run anyway**.

The round icon appears in the system tray (click the **^** next to the clock if it's hidden; you can drag it onto the taskbar to keep it visible).

## Settings

Right-click the tray icon → **Settings…** opens `%APPDATA%\DownloadKeepAwake\config.json` in Notepad. Save the file and changes apply within a few seconds — no restart needed.

```json
{
  "downloadManagers": ["fdm", "IDMan", "qbittorrent"],
  "minSpeedKBps": 64,
  "graceSeconds": 90,
  "checkSeconds": 3,
  "keepScreenOn": false,
  "notifications": true
}
```

| Setting | What it does | Default |
|---|---|---|
| `downloadManagers` | Programs to watch. Find the name in **Task Manager → Details** (`.exe` is optional). | see table above |
| `minSpeedKBps` | Activity slower than this counts as "not downloading". | `64` |
| `graceSeconds` | How long to stay awake after a download stops or stalls. | `90` |
| `checkSeconds` | How often to check. | `3` |
| `keepScreenOn` | Also stop the display from turning off while downloading. | `false` |
| `notifications` | Show a notification when keep-awake starts and stops. | `true` |

> Torrent clients keep uploading (seeding) after a download finishes, which would keep your PC awake. Add them only if that's what you want.

## How it works

Every few seconds the app reads Windows' per-process I/O counters for the download-manager processes. If they're moving more than `minSpeedKBps`, it tells Windows the system is in use via [`SetThreadExecutionState`](https://learn.microsoft.com/windows/win32/api/winbase/nf-winbase-setthreadexecutionstate) (`ES_SYSTEM_REQUIRED`) — the same mechanism video players and installers use. When activity has been below the threshold for `graceSeconds`, it releases that request and Windows' own sleep timer takes over again.

It prevents **automatic idle** sleep and hibernation. It deliberately does *not* override you: choosing Sleep from the Start menu, closing a laptop lid, or critical-battery hibernation still work as usual. You can see it working with `powercfg /requests` in an admin terminal.

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

Push a version tag and GitHub Actions builds both executables and publishes a release:

```sh
git tag v1.0.0
git push origin v1.0.0
```

## License

[MIT](LICENSE)

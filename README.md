<p align="center"><img src="assets/icon.png" width="96" alt=""></p>

# Download Keep-Awake

Download Keep-Awake is a small Windows 11 / 10 tray app that stops your PC from sleeping or hibernating **while anything is downloading**: browser downloads, download managers, Steam and other game launchers, torrents and Windows Update. When downloads finish, your normal sleep settings come back on their own.

## Install

1. Download **DownloadKeepAwake.exe** from the [latest release](../../releases/latest) (on Snapdragon / ARM PCs, use **DownloadKeepAwake-arm64.exe**).
2. Run it. It installs itself for your Windows account (no admin needed) into `%LOCALAPPDATA%\Programs\Download Keep-Awake`, adds itself to the Start menu and to **Settings → Apps → Installed apps**, and starts in the system tray.
3. On first run it asks whether to start with Windows.

The app isn't code-signed, so Windows SmartScreen warns on first install ("More info → Run anyway"). Updates install without that prompt.

To remove it, uninstall **Download Keep-Awake** from **Settings → Apps → Installed apps** like any other app.

## Using it

The round icon in the system tray (click **^** next to the clock if it's hidden) shows what it's doing:

- **Green**: something is downloading, so the PC is kept awake.
- **Grey**: nothing is downloading, so normal sleep applies.
- **Blue**: kept awake by hand (**Keep awake now** in the menu).
- **Amber**: paused (**Pause** in the menu).

Right-click it to see the current download speed and what was detected, switch each detector on or off, pause it, or open Settings.

### What counts as a download

The PC stays awake while **any** of these sees a download:

| Detector | Catches | How |
|---|---|---|
| **Any download (network traffic)** | Everything: browsers, Steam / Epic / Xbox / Battle.net, torrents, Windows Update, cloud sync, download managers | Incoming traffic on your real network adapters (Wi-Fi, Ethernet, mobile) above **150 KB/s**, averaged over 15 seconds |
| **Browser & torrent downloads** | Even slow downloads in Chrome, Edge, Brave, Opera, Firefox, qBittorrent, uTorrent, BitComet… | Unfinished download files still growing in your Downloads folder (`.crdownload`, `.part`, `.!qB`, `.!ut`…) |
| **Download manager apps** | IDM, Free Download Manager, Neat DM, JDownloader, XDM, Motrix, EagleGet, uGet, FlashGet, aria2, Persepolis | The app's own data activity above 64 KB/s |

Only downloading counts: uploading or torrent seeding doesn't keep your PC awake. VPN and virtual adapters are ignored so traffic isn't counted twice. After a download stops it waits 90 seconds before allowing sleep, so a short stall or a gap between files doesn't end the download. The screen can still turn off as normal.

HD video streaming counts as downloading too. If you don't want that, raise `networkMinSpeedKBps` in Settings or turn off "Detect any download" in the tray menu.

### Settings

**Settings…** in the tray menu opens `%APPDATA%\DownloadKeepAwake\config.json` in Notepad. Save it and changes apply within a few seconds.

| Setting | What it does | Default |
|---|---|---|
| `detectNetwork` | Detect any download from any app via network traffic. | `true` |
| `networkMinSpeedKBps` | Incoming speed that counts as downloading. | `150` |
| `networkWindowSeconds` | Speed is averaged over this many seconds, so brief bursts are ignored. | `15` |
| `detectPartialFiles` | Detect unfinished download files that are still growing. | `true` |
| `watchFolders` | Folders to watch, plus two levels of subfolders. `%DOWNLOADS%` is your Downloads folder wherever it is. Add your torrent or game download folders if they're elsewhere. | `["%DOWNLOADS%"]` |
| `partialExtensions` | File endings that mean "still downloading". | `.crdownload`, `.part`, `.!qB`, … |
| `detectApps` | Detect download manager programs. | `true` |
| `downloadManagers` | Programs to watch. Find the name in **Task Manager → Details** (`.exe` is optional). | see above |
| `minSpeedKBps` | Activity from those programs that counts as downloading. | `64` |
| `graceSeconds` | How long to stay awake after a download stops or stalls. | `90` |
| `checkSeconds` | How often to check. | `3` |
| `keepScreenOn` | Also keep the display on while downloading. | `false` |
| `notifications` | Show a notification when keep-awake starts and stops. | `true` |
| `checkForUpdates` | Look for a new version on launch and every 6 hours. | `true` |
| `autoInstallUpdates` | Install new versions by themselves. When off, you're asked first. | `true` |

## How updates work

Every time files change in this repository, GitHub builds the app (the **Build and release Download Keep-Awake** workflow under the **Actions** tab) and publishes it on the **Releases** page. Installed copies check that page shortly after launch and every six hours, and download the new version quietly. The download is checked against `SHA256SUMS.txt` from the same release; anything that doesn't match is thrown away.

- If nothing is downloading, the update installs straight away: the app restarts in about a second and shows **"Updated to x.y.z"**.
- If something is downloading, it shows **"Download Keep-Awake x.y.z is ready · Click here to restart now"** and otherwise installs by itself when your downloads finish. It also installs the next time the app closes.

You can also use **Check for updates** in the tray menu.

A build only publishes when the number in `VERSION` is higher than the last release. Each change should raise it: `1.3.0` → `1.3.1` for fixes, `1.4.0` for new features.

### Publishing a change from the GitHub website

1. Open the repository, click **Add file → Upload files**, and drop in the changed files, keeping their folders. Include `VERSION` with the new number.
2. Click **Commit changes**.
3. Open the **Actions** tab. The build takes about 2 minutes. A green tick means the new version is on the Releases page and installed copies will pick it up.

If a build fails, open it in the Actions tab; the red step shows what went wrong.

## Building on your own computer (optional)

You need [Go](https://go.dev/dl/) 1.22 or newer. Nothing else: the app talks to Windows directly.

```sh
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=1.3.0" -o DownloadKeepAwake.exe .
```

From Linux or macOS, put `GOOS=windows GOARCH=amd64` in front. To try a build without it installing itself, run it with `--portable`.

## Where things live

- `main.go` runs the tray icon, menu and the decision to keep the PC awake.
- `netmon.go` measures download speed on your network adapters.
- `partial.go` watches for unfinished download files.
- `monitor.go` watches download manager programs.
- `config.go` reads and writes `config.json` and the **Start with Windows** setting.
- `update.go` checks the Releases page, downloads, verifies and installs updates.
- `install.go` installs per-user, adds the Start menu shortcut and the Installed apps entry, and uninstalls.
- `icon.go` draws the coloured tray icons; `assets/icon.ico` and `assets/icon.png` are the app icon.
- `rsrc_windows_*.syso` embed the icon and Windows manifest into the `.exe`. If you change `assets/icon.ico` or `assets/app.manifest`, regenerate them with `go install github.com/akavel/rsrc@latest` then `go generate`.
- `.github/workflows/release.yml` is the automatic build.

## Notes

- It blocks **automatic** sleep and hibernation only, using the same Windows mechanism as video players (`SetThreadExecutionState`). Choosing Sleep from the Start menu, closing a laptop lid and critical-battery hibernation still work as usual. `powercfg /requests` in an admin terminal shows it while it's keeping the PC awake.
- Settings live in `%APPDATA%\DownloadKeepAwake` and are kept if you uninstall.

## License

[MIT](LICENSE)

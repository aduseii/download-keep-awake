<p align="center"><img src="assets/icon.png" width="96" alt=""></p>

# Download Keep-Awake

Download Keep-Awake is a small Windows 11 / 10 tray app that stops your PC from sleeping or hibernating **while Microsoft Edge, Free Download Manager or a game launcher is downloading**. When downloads finish, your normal sleep settings come back on their own. Nothing else (browsing, social media, streaming, other apps) keeps your PC awake.

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

Only these three, each of which can be switched on or off in the tray menu:

| Source | How it's detected |
|---|---|
| **Microsoft Edge** | While Edge is open, an unfinished download file (`.crdownload`) that's still growing. Edge's own download folder (Settings → Downloads → Location) is found automatically, along with your Windows Downloads folder. |
| **Free Download Manager** | FDM's own data activity staying above 64 KB/s for most of 15 seconds. |
| **Game launchers** | The same, for Steam, Epic Games, Xbox app, EA app, Battle.net, Ubisoft Connect, GOG Galaxy, Riot Client, Amazon Games, Rockstar Games Launcher and itch.io. Their built-in store pages (steamwebhelper and the like) don't count, so watching a trailer won't keep the PC awake. |

After a download stops, the app waits 90 seconds before allowing sleep, so a short stall or a gap between files doesn't end the download. The screen can still turn off as normal.

If Edge is set to **Ask me what to do with each download** and you save somewhere other than your Downloads folder or Edge's download folder, add that folder to `edgeFolders` in Settings.

### Settings

**Settings…** in the tray menu opens `%APPDATA%\DownloadKeepAwake\config.json` in Notepad. Save it and changes apply within a few seconds.

| Setting | What it does | Default |
|---|---|---|
| `detectEdge` | Watch Microsoft Edge downloads. | `true` |
| `edgeFolders` | Extra folders to watch for Edge downloads (plus two levels of subfolders). Edge's own download folder is always included. `%DOWNLOADS%` is your Windows Downloads folder. | `["%DOWNLOADS%"]` |
| `detectFreeDownloadManager` | Watch Free Download Manager. | `true` |
| `freeDownloadManagerProcesses` | FDM's program name in **Task Manager → Details**. | `["fdm"]` |
| `detectGameLaunchers` | Watch game launchers. | `true` |
| `gameLaunchers` | Launcher program names (from **Task Manager → Details**). Add one here if yours isn't listed. | Steam, Epic, Xbox, EA, … |
| `minSpeedKBps` | Steady activity from FDM or a launcher that counts as downloading. | `64` |
| `graceSeconds` | How long to stay awake after a download stops or stalls. | `90` |
| `checkSeconds` | How often to check. | `3` |
| `keepScreenOn` | Also keep the display on while downloading. | `false` |
| `notifications` | Show a notification when keep-awake starts and stops. | `true` |
| `checkForUpdates` | Look for a new version on launch and every 6 hours, and download it in the background. | `true` |
| `autoInstallUpdates` | Install new versions without asking. When off, you click **Update now**. | `false` |

## How updates work

**Getting an update is one click.** Installed copies check this repository's **Releases** page shortly after starting and every six hours. When there's a new version, they download it quietly in the background and check it against `SHA256SUMS.txt` from the same release; anything that doesn't match is thrown away. Then you see:

- a notification, **"Update ready: vX.Y.Z — click here to update now"**, and
- **Update now to vX.Y.Z** in bold at the top of the tray menu.

Click either one and the app swaps in the new version and restarts in about a second. Nothing installs until you click. **Check for updates** in the tray menu looks right away and, if there's something newer, downloads and installs it in the same click. If you'd rather never be asked, tick **Install updates without asking** and updates install by themselves whenever nothing is downloading.

**Publishing is automatic.** Every change pushed to the main branch is built by GitHub (the **Build and release Download Keep-Awake** workflow under the **Actions** tab, about a minute) and published as a new release, numbered automatically: the last release +1 (`1.4.0` → `1.4.1`). Nobody needs to edit a version number. To jump to a bigger number for a major change, put it in `VERSION` (for example `2.0.0`); it's used once it's higher than the last release. Changes to only the README or LICENSE don't make a release.

If a build fails, open it in the Actions tab; the red step shows what went wrong. Re-running it repairs that release instead of making a new one.

## Building on your own computer (optional)

You need [Go](https://go.dev/dl/) 1.22 or newer. Nothing else: the app talks to Windows directly.

```sh
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=1.5.0" -o DownloadKeepAwake.exe .
```

From Linux or macOS, put `GOOS=windows GOARCH=amd64` in front. To try a build without it installing itself, run it with `--portable`.

## Where things live

- `main.go` runs the tray icon, menu and the decision to keep the PC awake.
- `edge.go` finds Edge's download folders; `partial.go` watches for its unfinished `.crdownload` files.
- `monitor.go` measures how much data Free Download Manager and the game launchers are moving; `sustain.go` checks that it's steady.
- `config.go` reads and writes `config.json` and the **Start with Windows** setting.
- `update.go` checks the Releases page, downloads, verifies and installs updates.
- `install.go` installs per-user, adds the Start menu shortcut and the Installed apps entry, and uninstalls.
- `icon.go` draws the coloured tray icons; `assets/icon.ico` and `assets/icon.png` are the app icon.
- `rsrc_windows_*.syso` embed the icon and Windows manifest into the `.exe`. If you change `assets/icon.ico` or `assets/app.manifest`, regenerate them with `go install github.com/akavel/rsrc@latest` then `go generate`.
- `.github/workflows/release.yml` is the automatic build.

## Notes

- It blocks **automatic** sleep and hibernation only, using the same Windows mechanism as video players (`SetThreadExecutionState`). Choosing Sleep from the Start menu, closing a laptop lid and critical-battery hibernation still work as usual. `powercfg /requests` in an admin terminal shows it while it's keeping the PC awake.
- Settings live in `%APPDATA%\DownloadKeepAwake` and are kept if you uninstall.
- Microsoft Store and some Xbox app downloads are carried out by a Windows background service rather than the Xbox app itself, so those may not always be noticed.

## License

[MIT](LICENSE)

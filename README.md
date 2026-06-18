# Zensu

Zensu is a high-performance downloader for AnimePahe, featuring a premium glassmorphic dark user interface (powered by Wails + Vite/Vanilla JS) alongside a modular command-line interface (CLI) for advanced usage and terminal environments (including Android Termux).

---

## ✨ Features

- **Chrome TLS Fingerprinting:** Powered by `bogdanfinn/tls-client` to seamlessly bypass Cloudflare security layers.
- **Pure-Go Dean Edwards Deobfuscator:** 100% native Javascript packer decompression inside Go, removing runtime dependencies on Node.js or external script interpreters.
- **Self-Healing FFmpeg Engine:** If `ffmpeg` is not found on your system, Zensu automatically downloads and extracts the correct architecture-specific binary (stored in a local `./bin/` subfolder next to the binary) at runtime.
- **Network Glitch Resilience:**
  - **Direct Downloads:** Supports network-failure retries (up to 5 times) and connection resuming using HTTP `Range` headers.
  - **HLS Downloads:** Configures robust HLS segment reconnect options dynamically within FFmpeg.
- **AppData Configuration Migration:** Settings are saved directly to the user's OS-native configuration folders (`%APPDATA%/zensu/config.json` on Windows, and `~/.config/zensu/config.json` on Linux/Android) to protect config data across installations.
- **Dynamic Mirror/Domain Resolution:** Modify mirror domains directly in Settings to support AnimePahe domain changes without updating the app.
- **Intelligent Local Directory Scanning:** Checks your download directory and automatically flags completed/saved episodes in selection menus.

---

## 🔌 Chrome Extension Usage

The Chrome Extension allows you to easily sync clearance cookies from your browser to Zensu.

1. Open Chrome and navigate to `chrome://extensions/`.
2. Enable **Developer mode** (top-right toggle).
3. Click **Load unpacked** and select the `extension/` folder in this repository.
4. Navigate to `https://animepahe.pw`. 
5. Open the extension icon, copy the generated cookies, and paste them into Zensu settings.

---

## 💻 CLI Usage

Run the CLI for interactive terminal-based downloads:

* **Windows**: `build\bin\cli\zensu-cli.exe`
* **Linux / Termux**: `./build/bin/cli/zensu-cli` (or `./build/bin/cli/zensu-termux`)
* **How to Use**:
  1. Launch the executable.
  2. Type your search query and hit Enter.
  3. Select the anime from the list of search results.
  4. Select episodes to download (e.g., `1,2,3` or `1-5`).

---

## 🛠️ Build & Setup

To set up the development environment and compile Zensu:

1. **Initialize Environment**:
   - Windows: `.\setup.ps1`
   - Linux / Termux / macOS: `chmod +x setup.sh && ./setup.sh`
2. **Compile Targets**:
   - Run `./build.sh` to compile GUI and CLI binaries to the `build/bin/` folder.

---

## ⚙️ Configuration Settings

Configuration is initialized automatically. On first startup, the **Download Directory** (`downloadDir`) is automatically set to your user home `Videos/Anime` folder. To bypass Cloudflare blocks, you must update credentials via the Settings Tab (GUI) or your system's `config.json` file:
* **Windows path:** `%APPDATA%\zensu\config.json`
* **Linux/Android path:** `~/.config/zensu/config.json`

```json
{
  "ua": "Your browser User-Agent",
  "cf": "cf_clearance cookie value",
  "cookies": "Full cookie string from browser devtools",
  "downloadDir": "C:\\Users\\Username\\Videos\\Anime",
  "maxParallel": 3,
  "quality": "1080",
  "audio": "jpn",
  "domain": "https://animepahe.pw"
}
```

### Fetching Cookie Credentials manually (Alternative):
1. Open your browser and navigate to `animepahe.pw` (or your configured domain).
2. Press `F12` to open DevTools, select the **Network** tab, and reload the page.
3. Click any document request to the domain.
4. Copy the full `cookie:` request header and paste it as `"cookies"`.
5. Copy the individual `cf_clearance=...` cookie value and paste it as `"cf"`.
6. Copy the browser's User-Agent string and paste it as `"ua"`.

---

## 🐧 Linux & Android/Termux Setup

On Linux and Android (Termux), install `ffmpeg` before running the CLI:

* **Linux**: `sudo apt install ffmpeg`
* **Android (Termux)**: `pkg install ffmpeg`

Run:
```bash
chmod +x zensu-cli
./zensu-cli
```

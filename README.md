<p align="center">
  <img src="assets/zensu_banner.png" alt="Zensu Banner" width="100%" style="border-radius: 16px; box-shadow: 0 8px 30px rgba(0,0,0,0.5); border: 1px solid rgba(255,255,255,0.1);" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%20%7C%20JS-blueviolet?style=for-the-badge&logo=go&logoColor=white" alt="Language" />
  <img src="https://img.shields.io/badge/Framework-Wails%20v2-00F2FE?style=for-the-badge&logo=wails&logoColor=black" alt="Framework" />
  <img src="https://img.shields.io/badge/Bypass-Chrome%20CDP%20%7C%20TLS-ff4a5a?style=for-the-badge&logo=google-chrome&logoColor=white" alt="Bypass" />
  <img src="https://img.shields.io/badge/Server-Embedded%20%7C%20CORS%20Proxy-brightgreen?style=for-the-badge" alt="Server" />
</p>

<h1 align="center">
  <img src="assets/appicon.png" alt="Zensu Logo" width="70" style="border-radius: 14px; box-shadow: 0 4px 15px rgba(0,0,0,0.4); border: 1px solid rgba(255,255,255,0.08);" /><br>
  Zensu
</h1>
<p align="center"><b>A premium, glassmorphic dark-themed downloader & background streaming server for AnimePahe.</b><br>
Features native TLS fingerprinting, automated Cloudflare solver, modular multi-platform runtimes, and local HLS segment CORS proxying.
</p>

---

## ⚡ Highlights

<table width="100%">
  <tr>
    <td width="50%" valign="top">
      <h3>🚀 Pure Speed & Power</h3>
      <ul>
        <li><b>Concurrent Downloads:</b> Segmented parallel streams for maximum network utilization.</li>
        <li><b>TLS Fingerprinting:</b> Client hello simulation to slip past bot blockers undetected.</li>
        <li><b>HLS Resiliency:</b> Automatic recovery and dynamic fragment concatenation.</li>
        <li><b>Embedded Streaming Server:</b> Background server for live-streaming without prior downloading.</li>
      </ul>
    </td>
    <td width="50%" valign="top">
      <h3>🧠 Self-Healing System</h3>
      <ul>
        <li><b>Chrome CDP Solver:</b> Automated browser challenge solving to harvest clearance cookies.</li>
        <li><b>CORS Rewriter:</b> Bypasses browser CORS policy blocks by proxying and rewriting HLS playlists on-the-fly.</li>
        <li><b>Zero Node.js dependency:</b> Native Go deobfuscator unpacks javascript encoders instantly.</li>
        <li><b>Socket Port Transitioning:</b> Port changes dynamically restart active sockets in real-time.</li>
      </ul>
    </td>
  </tr>
</table>

---

## 🌀 Automatic Clearance Workflow

Zensu operates with a smart **self-healing authentication loop**. It checks connection health silently on startup, resolving clearance issues *only* when necessary:

```mermaid
graph TD
    A([Launch Zensu]) --> B{Test Connection}
    B -- "Clearance Valid (200 OK)" --> C[Ready to Search & Stream]
    B -- "Expired/Missing (403 Blocked)" --> D[Launch Debug Chrome]
    D --> E[User/Auto Solves Cloudflare Challenge]
    E --> F[Extract cf_clearance & User-Agent]
    F --> G[Save Config & Terminate Chrome]
    G --> C
    
    style A fill:#1a1b26,stroke:#7aa2f7,stroke-width:2px,color:#fff
    style B fill:#1a1b26,stroke:#e0af68,stroke-width:2px,color:#fff
    style C fill:#1a1b26,stroke:#9ece6a,stroke-width:2px,color:#fff
    style D fill:#1a1b26,stroke:#f7768e,stroke-width:2px,color:#fff
    style G fill:#1a1b26,stroke:#73daca,stroke-width:2px,color:#fff
```

---

## 📺 Local HTTP Streaming Server

Zensu embeds a high-performance **HTTP Streaming Server** directly in the application backend (which is also compileable as a standalone CLI target). It serves as a middleman proxy that streams media packets to external media players (such as VLC, MPV, ExoPlayer, or custom web interfaces).

### ⚙️ Features
* **On-the-Fly Range Seeking:** Forwarding of incoming HTTP `Range` headers to the source CDNs, enabling smooth scrubbing and zero-latency timeline navigation.
* **CORS Playlist Rewriter:** Resolves browser security blocks by reading `.m3u8` playlists and rewriting segment and key links to point back to the local proxy.
* **Dynamic GUI Socket Control:** Instantly starts, stops, or migrates to a different server port directly from the settings panel.

### 🔗 Stream Resolution Sequence

```mermaid
sequenceDiagram
    participant Player as Streaming Client (e.g., VLC, Web Player)
    participant Server as Zensu Server (Local)
    participant Kwik as Kwik Redirector
    participant CDN as Anime CDN (e.g., uwucdn.top)

    Player->>Server: GET /api/stream?slug=...&session=...
    Server->>Kwik: Resolve Embed URL (User-Agent, Cookies)
    Kwik-->>Server: Return HLS Playlist (.m3u8) URL
    Server->>CDN: Download Playlist
    CDN-->>Server: Return Playlist Content
    Note over Server: Rewrites absolute/relative segment<br/>URLs to point to Local Server Proxy
    Server-->>Player: Return Rewritten Playlist (.m3u8) with CORS Headers
    
    Player->>Server: GET /api/stream?proxy_url=CDN_segment.ts
    Server->>CDN: Request segment (forward User-Agent, Referer, Range)
    CDN-->>Server: Segment bytes (206 Partial Content)
    Server-->>Player: Stream segment bytes with Local CORS headers
```

### 🛰️ API Endpoints

#### `GET /api/search?q=<query>`
Searches anime titles. Returns matched sessions and poster URLs.

#### `GET /api/episodes?slug=<anime-session-slug>`
Retrieves the full index of releases and stream keys for the specified anime.

#### `GET /api/stream?slug=<slug>&session=<episode-session>&title=<optional>`
Serves the stream data.
* If the episode is already downloaded to the local directory (matched via the optional `title`), it is streamed instantly from your hard drive via zero-latency Go static file serving.
* If not downloaded, it dynamically proxies and streams remote segment packets while rewriting playlist files.

---

## 🛠️ Build & Installation

Get Zensu running on your machine with a few terminal commands:

> [!NOTE]
> Ensure you have Go v1.22+ and Node.js installed to build from source.

### 1. Initialize Environment
Choose the setup script matching your operating system:
* **Windows (PowerShell)**: 
  ```powershell
  .\setup.ps1
  ```
* **Linux / Termux / macOS (Shell)**: 
  ```bash
  chmod +x setup.sh && ./setup.sh
  ```

### 2. Compile Targets
* **Windows (PowerShell)**: 
  ```powershell
  $env:PATH = "C:\Program Files\Go\bin;" + $env:PATH; .\build.ps1
  ```
* **Linux / macOS (Shell)**: 
  ```bash
  ./build.sh
  ```

All compiled binaries will be built into the `build/bin/` and `build/bin/cli/` directories.

---

## 🎮 Interface Modes

### 🪐 Desktop GUI
Launch the visual binary: `.\build\bin\zensu.exe`
* **Zero-Click Bypass:** Verifies cookies on startup, opening Chrome in the background only if clearance expired.
* **Streaming settings:** Change streaming port, toggle automatic server startup, or manually restart the background listener.
* **Glassmorphic UI:** A dark, visually pleasing user interface styled for fluid animations.

### 💻 Standalone Server Binary
Run the dedicated backend target on a terminal server, NAS, or remote VPS:
* **Windows**: `build\bin\cli\zensu-server.exe`
* **Linux**: `./build/bin/cli/zensu-server`
* **Android/Termux**: `./build/bin/cli/zensu-server-termux`

### 🖥️ Client Downloader CLI
For lightweight terminal downloading:
* **Windows**: `build\bin\cli\zensu-cli.exe`
* **Linux**: `./build/bin/cli/zensu-cli`
* **Android/Termux**: `./build/bin/cli/zensu-termux`

---

## ⚙️ App Configurations

Your preferences are managed automatically in OS-native app data directories:
* **Windows path:** `%APPDATA%\zensu\config.json`
* **Linux/Android path:** `~/.config/zensu/config.json`

```json
{
  "ua": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36...",
  "cf": "your_cf_clearance_token",
  "downloadDir": "C:\\Users\\User\\Videos\\Anime",
  "maxParallel": 3,
  "quality": "1080",
  "audio": "jpn",
  "domain": "https://animepahe.pw",
  "serverPort": 8080,
  "serverAutoStart": true
}
```

---

## 📂 Developer Bypass Fallbacks

If automatic Chrome CDP extraction is not preferred, Zensu offers two alternative manual clearance flows:

<details>
<summary><b>Option A: Chrome Extension (Click to Expand)</b></summary>
<br>

1. Open Chrome and navigate to `chrome://extensions/`.
2. Enable **Developer mode** toggle (top-right).
3. Click **Load unpacked** and select the `extension/` directory.
4. Visit `https://animepahe.pw`, open the Zensu extension popup, and copy the cookie payloads.
</details>

<details>
<summary><b>Option B: Manual Header Copy (Click to Expand)</b></summary>
<br>

1. Navigate to your mirror domain, press `F12` for DevTools.
2. Select the **Network** tab and reload the page.
3. Select any document request, and copy the `cookie:` header string, `cf_clearance` value, and `User-Agent`.
4. Paste them directly into the GUI settings panel or your `config.json` file.
</details>

# Deploying Zensu Server on an Ubuntu VPS

This guide explains how to deploy the Zensu Streaming Server on an Ubuntu VPS. It uses a **real Google Chrome browser** running on a virtual display (**Xvfb**) with Chrome DevTools Protocol (CDP), so Cloudflare challenges and cookies are handled automatically just like on a Windows desktop.

---

## Method 1: Docker Deployment (Recommended)

Docker provides a completely self-contained environment with Google Chrome, Xvfb, noVNC, and the compiled Zensu server.

### 1. Prerequisites on Ubuntu VPS

Ensure Docker and Docker Compose are installed:
```bash
sudo apt update && sudo apt install -y curl
curl -fsSL https://get.docker.com | sh
```

### 2. Copy Project Files to the VPS

You can clone your repository or copy the project files to the VPS:
```bash
# Example using scp or git
git clone <your-repo-url> /opt/zensu
cd /opt/zensu
```

### 3. Launch the Server

Run Docker Compose:
```bash
docker compose -f docker-compose.server.yml up -d --build
```

### 4. Verify & Monitor

- **Check logs:**
  ```bash
  docker compose -f docker-compose.server.yml logs -f
  ```

- **Streaming API Endpoint:**
  ```
  http://<YOUR_VPS_IP>:8080
  ```
  Test search endpoint:
  ```bash
  curl "http://<YOUR_VPS_IP>:8080/api/search?q=one+piece"
  ```

- **Remote Browser View (noVNC):**
  If Cloudflare ever presents an interactive "Verify you are human" checkbox that requires clicking, you can view the actual Chrome window by opening:
  ```
  http://<YOUR_VPS_IP>:6080/vnc.html
  ```
  Click **Connect** to see and interact with the virtual browser screen in real time!

---

## Method 2: Native Ubuntu (Systemd Service)

If you prefer running directly on Ubuntu without Docker:

### 1. Install Google Chrome & Xvfb

```bash
sudo apt update
sudo apt install -y wget curl xvfb libnss3 libatk-bridge2.0-0 libgtk-3-0 libasound2

# Install official Google Chrome
wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
sudo apt install -y ./google-chrome-stable_current_amd64.deb
rm google-chrome-stable_current_amd64.deb
```

### 2. Copy the Linux Binary

Copy the Linux binary compiled from your build:
- Located at: `build/bin/cli/zensu-server`
- Copy it to the VPS at `/usr/local/bin/zensu-server`:
  ```bash
  sudo chmod +x /usr/local/bin/zensu-server
  ```

### 3. Create a Systemd Service

Create `/etc/systemd/system/zensu-server.service`:
```ini
[Unit]
Description=Zensu Streaming Server with Xvfb
After=network.target

[Service]
Type=simple
User=root
# xvfb-run creates a virtual X11 display automatically for Chrome
ExecStart=/usr/bin/xvfb-run --auto-servernum --server-args="-screen 0 1920x1080x24" /usr/local/bin/zensu-server
Restart=always
RestartSec=5
KillMode=process

[Install]
WantedBy=multi-user.target
```

### 4. Enable and Start the Service

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now zensu-server
```

Check status and logs:
```bash
sudo systemctl status zensu-server
journalctl -u zensu-server -f
```

---

## How Cookie Refresh Works on VPS

1. **At Startup**: If `cf_clearance` or User-Agent is missing or invalid, Zensu launches the real Google Chrome on the virtual display (`DISPLAY=:99`), solves the challenge via CDP, saves the new cookies to `~/.config/zensu/config.json`, and starts listening on port 8080.
2. **At Runtime**: If Cloudflare expires clearance cookies during any stream/search request or during background health checks:
   - Zensu detects the expiration.
   - Automatically launches Chrome on the virtual screen.
   - Polls and captures the new `cf_clearance` cookie.
   - Retries the request seamlessly without crashing.

#!/bin/bash
set -e

DISPLAY_NUM=99
export DISPLAY=":${DISPLAY_NUM}"
export DOCKER=1
export CONTAINER=1

echo "=========================================================="
echo " Starting Zensu Server Container"
echo " Virtual Display: ${DISPLAY}"
echo "=========================================================="

# 1. Clean up any leftover X11 lock files from unclean restarts
rm -f /tmp/.X${DISPLAY_NUM}-lock /tmp/.X11-unix/X${DISPLAY_NUM} 2>/dev/null || true

# 2. Start Xvfb (Virtual FrameBuffer)
echo "[INFO] Starting Xvfb on display ${DISPLAY} (1920x1080x24)..."
Xvfb "${DISPLAY}" -screen 0 1920x1080x24 -ac +extension GLX +render -noreset &
XVFB_PID=$!

# Wait for Xvfb to become ready
for i in $(seq 1 10); do
    if [ -e "/tmp/.X11-unix/X${DISPLAY_NUM}" ]; then
        break
    fi
    sleep 0.5
done

# 3. Optional: Start x11vnc and noVNC (web browser access to the virtual screen)
if [ "${ENABLE_VNC:-true}" = "true" ]; then
    echo "[INFO] Starting x11vnc on port 5900..."
    if [ -n "${VNC_PASSWORD}" ]; then
        mkdir -p ~/.vnc
        x11vnc -storepasswd "${VNC_PASSWORD}" ~/.vnc/passwd
        x11vnc -display "${DISPLAY}" -forever -shared -rfbauth ~/.vnc/passwd -rfbport 5900 -bg 2>/dev/null
    else
        x11vnc -display "${DISPLAY}" -forever -shared -nopw -rfbport 5900 -bg 2>/dev/null
    fi

    echo "[INFO] Starting noVNC web interface on port 6080..."
    /usr/share/novnc/utils/novnc_proxy --vnc localhost:5900 --listen 6080 > /dev/null 2>&1 &
    echo "[INFO] noVNC is accessible at http://<VPS_IP>:6080/vnc.html"
fi

# 4. Graceful shutdown handler
cleanup() {
    echo "[INFO] Shutting down container services..."
    kill -TERM "$SERVER_PID" 2>/dev/null || true
    kill -TERM "$XVFB_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    exit 0
}
trap cleanup SIGTERM SIGINT

# 5. Start Zensu Streaming Server
echo "[INFO] Launching Zensu Server..."
/app/zensu-server &
SERVER_PID=$!

wait "$SERVER_PID"

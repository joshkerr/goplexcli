#!/bin/sh
# Install the goplexcli desktop GUI for the current user on Linux:
# binary -> ~/.local/bin, icon + .desktop entry -> ~/.local/share.
# Usage: install-gui-linux.sh <path-to-built-binary> <path-to-appicon.png>
set -e

SOURCE="${1:?usage: install-gui-linux.sh <binary> <icon.png>}"
ICON="${2:?usage: install-gui-linux.sh <binary> <icon.png>}"

BIN_DIR="${HOME}/.local/bin"
ICON_ROOT="${HOME}/.local/share/icons/hicolor"
DESKTOP_DIR="${HOME}/.local/share/applications"

mkdir -p "$BIN_DIR" "$DESKTOP_DIR"

install -m 0755 "$SOURCE" "$BIN_DIR/goplexcli-gui"

# Install the icon at the standard hicolor sizes. The theme index only lists
# sizes up to 512x512, so a lone 1024x1024 copy is never found by name and
# GNOME falls back to a generic icon. Scale with Pillow when available;
# otherwise drop the source PNG into 512x512 and let GTK scale it down.
rm -f "$ICON_ROOT/1024x1024/apps/goplexcli-gui.png"
if python3 -c 'import PIL' >/dev/null 2>&1; then
    python3 - "$ICON" "$ICON_ROOT" <<'PY'
import os, sys
from PIL import Image
src, root = sys.argv[1], sys.argv[2]
img = Image.open(src).convert("RGBA")
for size in (512, 256, 128, 64, 48, 32, 16):
    d = os.path.join(root, f"{size}x{size}", "apps")
    os.makedirs(d, exist_ok=True)
    img.resize((size, size), Image.LANCZOS).save(os.path.join(d, "goplexcli-gui.png"))
PY
else
    mkdir -p "$ICON_ROOT/512x512/apps"
    install -m 0644 "$ICON" "$ICON_ROOT/512x512/apps/goplexcli-gui.png"
fi

cat > "$DESKTOP_DIR/goplexcli-gui.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=goplexcli
Comment=Browse and play your Plex library
Exec=$BIN_DIR/goplexcli-gui
Icon=goplexcli-gui
Terminal=false
Categories=AudioVideo;Network;
EOF

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t --ignore-theme-index "$ICON_ROOT" >/dev/null 2>&1 || true
fi

echo "Installed goplexcli GUI to $BIN_DIR/goplexcli-gui"
echo "Created launcher: $DESKTOP_DIR/goplexcli-gui.desktop"

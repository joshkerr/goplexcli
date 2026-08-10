"""Generate desktop icons from the approved Go Plex glass mark.

The 1024px ``../assets/appicon-source.png`` is the reviewed full-bleed artwork
shared with goplexcli-ios. Desktop outputs receive the transparent rounded-
square mask expected by macOS, Windows, and Linux launchers.

Outputs:
  appicon.png        1024px Wails/macOS/Linux icon
  windows/icon.ico   multi-size Windows icon
  icons/preview.png  256px quick-look preview
  ../frontend/public/appicon.png 256px React splash/brand mark
"""

import os
from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
SOURCE = os.path.normpath(
    os.path.join(HERE, "..", "assets", "appicon-source.png")
)
OUT_ICONS = os.path.join(HERE, "icons")
OUT_WINDOWS = os.path.join(HERE, "windows")
OUT_PUBLIC = os.path.normpath(os.path.join(HERE, "..", "frontend", "public"))
CORNER_RADIUS = 0.225


def render(source, size):
    """Resize the approved artwork and apply a desktop squircle alpha mask."""
    image = source.resize((size, size), Image.Resampling.LANCZOS)
    supersample = 4
    mask_size = size * supersample
    mask = Image.new("L", (mask_size, mask_size), 0)
    ImageDraw.Draw(mask).rounded_rectangle(
        (0, 0, mask_size - 1, mask_size - 1),
        radius=round(mask_size * CORNER_RADIUS),
        fill=255,
    )
    image.putalpha(mask.resize((size, size), Image.Resampling.LANCZOS))
    return image


def main():
    source = Image.open(SOURCE).convert("RGBA")
    if source.size != (1024, 1024):
        raise ValueError("appicon-source.png must be exactly 1024x1024 pixels")

    os.makedirs(OUT_ICONS, exist_ok=True)
    os.makedirs(OUT_WINDOWS, exist_ok=True)
    os.makedirs(OUT_PUBLIC, exist_ok=True)

    render(source, 1024).save(os.path.join(HERE, "appicon.png"))
    preview = render(source, 256)
    preview.save(os.path.join(OUT_ICONS, "preview.png"))
    preview.save(os.path.join(OUT_PUBLIC, "appicon.png"))

    sizes = (256, 128, 64, 48, 32, 24, 16)
    icons = {size: render(source, size) for size in sizes}
    icons[256].save(
        os.path.join(OUT_WINDOWS, "icon.ico"),
        format="ICO",
        append_images=[icons[size] for size in sizes[1:]],
        sizes=[(size, size) for size in sizes],
    )
    print(
        "wrote appicon.png, windows/icon.ico, icons/preview.png, "
        "frontend/public/appicon.png"
    )


if __name__ == "__main__":
    main()

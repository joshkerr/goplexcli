"""Generate goplexcli GUI app icons (3 variants) with PIL.

Design goals: full-bleed rounded-square so the icon reads large in
taskbar/dock; bold single glyph that survives 16px; Plex-gold palette.

Variant A (recommended): dark squircle, gold downward play-triangle over a
tray bar -- reads as both "media" (play) and "download" (arrow into tray).
Variant B: dark squircle, classic right-facing gold play triangle.
Variant C: inverted -- gold squircle, dark glyph of A.
"""

import math
import os
from PIL import Image, ImageDraw, ImageFilter

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "icons")

GOLD_TOP = (247, 199, 74)     # #F7C74A
GOLD_MID = (229, 160, 13)     # #E5A00D (Plex gold)
GOLD_BOT = (198, 128, 8)      # #C68008
DARK_TOP = (42, 47, 58)       # #2A2F3A
DARK_BOT = (18, 21, 27)       # #12151B
GLYPH_DARK = (24, 27, 34)     # dark glyph for variant C


def lerp(a, b, t):
    return tuple(round(a[i] + (b[i] - a[i]) * t) for i in range(3))


def vgrad(size, stops):
    """Vertical gradient image from [(pos, rgb), ...] stops."""
    im = Image.new("RGB", (1, size))
    px = im.load()
    for y in range(size):
        t = y / max(1, size - 1)
        for (p0, c0), (p1, c1) in zip(stops, stops[1:]):
            if t <= p1 or (p1 == stops[-1][0]):
                if p1 == p0:
                    px[0, y] = c0
                else:
                    tt = min(1.0, max(0.0, (t - p0) / (p1 - p0)))
                    px[0, y] = lerp(c0, c1, tt)
                if t <= p1:
                    break
    return im.resize((size, size))


def rounded_poly(points, radius, steps=24):
    """Round polygon corners with arcs of `radius`; returns point list."""
    n = len(points)
    out = []
    for i in range(n):
        a = points[(i - 1) % n]
        b = points[i]
        c = points[(i + 1) % n]
        ux, uy = a[0] - b[0], a[1] - b[1]
        vx, vy = c[0] - b[0], c[1] - b[1]
        lu = math.hypot(ux, uy)
        lv = math.hypot(vx, vy)
        ux, uy = ux / lu, uy / lv if False else uy / lu
        vx, vy = vx / lv, vy / lv
        # angle between edges at b
        dot = ux * vx + uy * vy
        ang = math.acos(max(-1.0, min(1.0, dot)))
        r = min(radius, 0.45 * min(lu, lv) * math.tan(ang / 2))
        d = r / math.tan(ang / 2)
        p1 = (b[0] + ux * d, b[1] + uy * d)
        p2 = (b[0] + vx * d, b[1] + vy * d)
        # arc center
        bx, by = ux + vx, uy + vy
        lb = math.hypot(bx, by)
        oc = (b[0] + bx / lb * (r / math.sin(ang / 2)),
              b[1] + by / lb * (r / math.sin(ang / 2)))
        a1 = math.atan2(p1[1] - oc[1], p1[0] - oc[0])
        a2 = math.atan2(p2[1] - oc[1], p2[0] - oc[0])
        # sweep the short way
        da = a2 - a1
        while da > math.pi:
            da -= 2 * math.pi
        while da < -math.pi:
            da += 2 * math.pi
        for s in range(steps + 1):
            t = a1 + da * s / steps
            out.append((oc[0] + r * math.cos(t), oc[1] + r * math.sin(t)))
    return out


def glyph_mask(size, variant, scale=1.0):
    """L-mode mask of the glyph for a canvas of `size`."""
    m = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(m)
    S = size

    def sc(v):  # scale around center
        return 0.5 + (v - 0.5) * scale

    if variant in ("a", "c"):
        # downward play triangle
        tri = [(sc(0.225) * S, sc(0.235) * S),
               (sc(0.775) * S, sc(0.235) * S),
               (sc(0.5) * S, sc(0.60) * S)]
        d.polygon(rounded_poly(tri, 0.045 * S * scale), fill=255)
        # tray bar
        y0, y1 = sc(0.685) * S, sc(0.765) * S
        x0, x1 = sc(0.225) * S, sc(0.775) * S
        d.rounded_rectangle([x0, y0, x1, y1],
                            radius=(y1 - y0) / 2, fill=255)
    else:
        # right-facing play triangle, optically centered (nudged right)
        tri = [(sc(0.335) * S, sc(0.22) * S),
               (sc(0.335) * S, sc(0.78) * S),
               (sc(0.80) * S, sc(0.5) * S)]
        d.polygon(rounded_poly(tri, 0.055 * S * scale), fill=255)
    return m


def render(size, variant="a"):
    ss = 8 if size <= 64 else 4
    S = size * ss
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))

    dark_bg = variant in ("a", "b")
    margin = 0.0 * S
    rad = 0.225 * S

    # background squircle
    bg_mask = Image.new("L", (S, S), 0)
    ImageDraw.Draw(bg_mask).rounded_rectangle(
        [margin, margin, S - 1 - margin, S - 1 - margin], radius=rad, fill=255)
    if dark_bg:
        bg = vgrad(S, [(0.0, DARK_TOP), (1.0, DARK_BOT)])
    else:
        bg = vgrad(S, [(0.0, GOLD_TOP), (0.55, GOLD_MID), (1.0, GOLD_BOT)])
    img.paste(bg, (0, 0), bg_mask)

    # subtle top inner highlight
    hl = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    hd = ImageDraw.Draw(hl)
    hd.rounded_rectangle([margin + S * 0.004, margin + S * 0.004,
                          S - 1 - margin - S * 0.004, S * 0.55],
                         radius=rad * 0.98,
                         outline=(255, 255, 255, 26 if dark_bg else 60),
                         width=max(1, round(S * 0.006)))
    fade = Image.new("L", (S, S), 0)
    ImageDraw.Draw(fade).rectangle([0, 0, S, S * 0.30], fill=255)
    fade = fade.filter(ImageFilter.GaussianBlur(S * 0.08))
    hl.putalpha(Image.composite(hl.getchannel("A"), Image.new("L", (S, S), 0), fade))
    img = Image.alpha_composite(img, hl)

    # glyph: bigger at tiny sizes for legibility
    gscale = 1.12 if size <= 32 else 1.0
    gm = glyph_mask(S, variant, gscale)
    gm = Image.composite(gm, Image.new("L", (S, S), 0), bg_mask)  # clip to bg

    # soft drop shadow (skip at tiny sizes)
    if size >= 48:
        sh = Image.new("RGBA", (S, S), (0, 0, 0, 0))
        black = Image.new("RGBA", (S, S), (0, 0, 0, 110 if dark_bg else 70))
        sh.paste(black, (0, round(S * 0.018)), gm)
        sh = sh.filter(ImageFilter.GaussianBlur(S * 0.02))
        img = Image.alpha_composite(img, sh)

    if dark_bg:
        fill = vgrad(S, [(0.0, GOLD_TOP), (0.5, GOLD_MID), (1.0, GOLD_BOT)])
    else:
        fill = Image.new("RGB", (S, S), GLYPH_DARK)
    glyph = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    glyph.paste(fill, (0, 0), gm)
    img = Image.alpha_composite(img, glyph)

    return img.resize((size, size), Image.LANCZOS)


def main():
    os.makedirs(OUT, exist_ok=True)
    for v in ("a", "b", "c"):
        vd = os.path.join(OUT, v)
        os.makedirs(vd, exist_ok=True)
        render(1024, v).save(os.path.join(vd, "appicon.png"))
        for s in (256, 128, 64, 48, 32, 24, 16):
            render(s, v).save(os.path.join(vd, f"icon_{s}.png"))
        # multi-size ico from individually rendered sizes
        imgs = {s: render(s, v) for s in (16, 24, 32, 48, 64, 128, 256)}
        base = imgs[256]
        base.save(os.path.join(vd, "icon.ico"), format="ICO",
                  append_images=[imgs[s] for s in (128, 64, 48, 32, 24, 16)],
                  sizes=[(s, s) for s in (256, 128, 64, 48, 32, 24, 16)])
        print("done", v)


if __name__ == "__main__":
    main()

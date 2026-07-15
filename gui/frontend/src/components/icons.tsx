// Minimal inline SVG icon set (stroke-based, currentColor) so the UI ships no
// icon-font dependency.
import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement>;

const base = (props: IconProps) => ({
  width: 20,
  height: 20,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  ...props,
});

export const FilmIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <rect x="3" y="4" width="18" height="16" rx="2" />
    <path d="M7 4v16M17 4v16M3 9h4M3 15h4M17 9h4M17 15h4" />
  </svg>
);

// BrandMark is the app logo: a dark squircle tile with the play-triangle + tray
// glyph, filled with the cyan -> violet -> pink -> red brand gradient and a
// neon glow, mirroring the desktop app icon (see gui/build/gen_icons.py).
export const BrandMark = (p: IconProps) => (
  <svg width={60} height={60} viewBox="0 0 64 64" fill="none" {...p}>
    <defs>
      <linearGradient id="bmTile" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#1b2230" />
        <stop offset="1" stopColor="#0b0e15" />
      </linearGradient>
      <linearGradient
        id="bmGlyph"
        gradientUnits="userSpaceOnUse"
        x1="15"
        y1="15"
        x2="49"
        y2="49"
      >
        <stop offset="0" stopColor="#2ECAFF" />
        <stop offset="0.4" stopColor="#6976F2" />
        <stop offset="0.7" stopColor="#E24ACD" />
        <stop offset="1" stopColor="#FF4A58" />
      </linearGradient>
      <filter id="bmGlow" x="-50%" y="-50%" width="200%" height="200%">
        <feGaussianBlur stdDeviation="2.4" />
      </filter>
    </defs>
    <rect
      x="1"
      y="1"
      width="62"
      height="62"
      rx="15"
      fill="url(#bmTile)"
      stroke="rgba(255,255,255,0.08)"
    />
    <g
      fill="url(#bmGlyph)"
      stroke="url(#bmGlyph)"
      strokeWidth="2.2"
      strokeLinejoin="round"
    >
      <g filter="url(#bmGlow)" opacity="0.85">
        <path d="M14.4 15 H49.6 L32 38.4 Z" />
        <rect x="14.4" y="43.8" width="35.2" height="5.4" rx="2.7" />
      </g>
      <path d="M14.4 15 H49.6 L32 38.4 Z" />
      <rect x="14.4" y="43.8" width="35.2" height="5.4" rx="2.7" />
    </g>
  </svg>
);

export const TvIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <rect x="3" y="6" width="18" height="12" rx="2" />
    <path d="M8 21h8M12 18v3" />
  </svg>
);

export const SparkIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5 18 18M18 6l-2.5 2.5M8.5 15.5 6 18" />
  </svg>
);

export const PlayIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M7 5v14l11-7z" fill="currentColor" stroke="none" />
  </svg>
);

export const ResumeIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M3 12a9 9 0 1 0 3-6.7M3 4v4h4" />
    <path d="M11 9v6l5-3z" fill="currentColor" stroke="none" />
  </svg>
);

export const DownloadIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M12 3v12M7 11l5 4 5-4M5 21h14" />
  </svg>
);

export const SearchIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.2-3.2" />
  </svg>
);

export const CloseIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M6 6l12 12M18 6 6 18" />
  </svg>
);

export const SettingsIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
  </svg>
);

export const StackIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M12 3 2 8l10 5 10-5z" />
    <path d="M2 13l10 5 10-5M2 18l10 5 10-5" />
  </svg>
);

export const StarIcon = (p: IconProps) => (
  <svg {...base(p)}>
    <path
      d="m12 3 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9 6.8 19.2l1-5.8L3.5 9.2l5.9-.9z"
      fill="currentColor"
      stroke="none"
    />
  </svg>
);

export const ChevronLeft = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M15 6l-6 6 6 6" />
  </svg>
);

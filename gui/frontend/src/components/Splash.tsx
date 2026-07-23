import type { ReactNode } from "react";
import { BrandMark } from "./icons";

/**
 * Full-screen startup splash: the brand mark breathing over a soft animated
 * glow in the logo's gradient colors, with the wordmark beneath. Children
 * render below it — the pulsing "Loading…" line, or the startup-error panel.
 */
export function Splash({ children }: { children?: ReactNode }) {
  return (
    <div className="relative flex h-full flex-col items-center justify-center overflow-hidden bg-ink-900 px-8 text-center animate-fade-in">
      {/* Ambient glow: a blurred blob in the brand-gradient colors. Sits
          behind the logo; pointer-events off so it never eats clicks on the
          error panel's Retry button. */}
      <div className="pointer-events-none absolute h-80 w-80 animate-glow rounded-full bg-gradient-to-br from-[#2ECAFF]/25 via-[#6976F2]/20 to-[#FF4A58]/25 blur-3xl" />

      <div className="relative animate-breathe">
        <BrandMark width={112} height={112} />
      </div>

      <div className="relative mt-7 leading-tight">
        <div className="text-2xl font-semibold tracking-tight text-white">
          Goplex
        </div>
        <div className="mt-1 text-[11px] font-medium uppercase tracking-widest text-white/40">
          Media
        </div>
      </div>

      {children && <div className="relative mt-9">{children}</div>}
    </div>
  );
}

import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { SlidersIcon } from "./icons";

interface Props {
  label: string;
  // Filters currently hiding titles. Shown on the button so a filter tucked
  // away in here never makes the library look like it's missing items.
  active: number;
  // Puts everything in here back to its default; disabled when nothing differs.
  onReset: () => void;
  canReset: boolean;
  children: ReactNode;
}

/** The grid's filter and sort controls, behind one button in the content
 * header. The button is a real <button>, so Enter and Space open and close it;
 * opening moves focus to the first control inside. Escape closes it and gives
 * focus back to the button, as does a press outside it (mouse or finger) or
 * tabbing out of it. A Select open inside takes Escape first: it catches it on
 * window in the capture phase, before this panel's document-level handler. */
export function FilterMenu({ label, active, onReset, canReset, children }: Props) {
  const id = useId();
  const btnRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  const close = (refocus: boolean) => {
    setOpen(false);
    if (refocus) btnRef.current?.focus({ preventScroll: true });
  };

  useEffect(() => {
    if (!open) return;
    panelRef.current
      ?.querySelector<HTMLElement>("button:not(:disabled), input, [tabindex]")
      ?.focus({ preventScroll: true });
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (btnRef.current?.contains(t) || panelRef.current?.contains(t)) return;
      close(false);
    };
    // Marked handled so the detail drawer's Escape handler leaves it be.
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      e.preventDefault();
      close(true);
    };
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open]);

  // Tabbing past the last control (or back past the button) closes it. A
  // click on the panel's padding blurs to nothing, which isn't leaving.
  const onBlur = (e: React.FocusEvent) => {
    const to = e.relatedTarget as Node | null;
    if (!to || btnRef.current?.contains(to) || panelRef.current?.contains(to)) return;
    setOpen(false);
  };

  const panelId = `${id}-panel`;
  return (
    <div className="relative" onBlur={onBlur}>
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        title={active > 0 ? `${label} — ${active} active` : label}
        style={{ ["--wails-draggable" as any]: "no-drag" }}
        className={`touch-target relative inline-flex items-center gap-2 rounded-lg border bg-ink-700 px-3 py-1.5 text-sm font-medium outline-none transition-colors focus-visible:border-accent/60 ${
          open || active > 0
            ? "border-accent/50 text-accent-soft"
            : "border-white/10 text-white/70 hover:border-accent/50 hover:text-white"
        }`}
      >
        <SlidersIcon width={15} height={15} />
        {label}
        {active > 0 && (
          <span className="min-w-[1.125rem] rounded-full bg-accent px-1 text-center text-[10px] font-bold leading-[1.125rem] text-ink-900">
            {active}
          </span>
        )}
      </button>
      {open && (
        <div
          ref={panelRef}
          id={panelId}
          role="dialog"
          aria-label={label}
          style={{ ["--wails-draggable" as any]: "no-drag" }}
          className="absolute right-0 top-full z-40 mt-2 w-64 rounded-xl border border-white/10 bg-ink-600 p-3 shadow-card animate-fade-in"
        >
          {children}
          <div className="mt-3 flex justify-end border-t border-white/5 pt-3">
            <button
              type="button"
              onClick={onReset}
              disabled={!canReset}
              className="touch-target relative rounded-lg px-3 py-1.5 text-sm font-medium text-accent-soft transition-colors hover:bg-accent/15 disabled:text-white/30 disabled:hover:bg-transparent"
            >
              Reset
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

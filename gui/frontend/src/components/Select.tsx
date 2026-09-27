import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { ChevronDownIcon } from "./icons";

export type SelectOption<T extends string | number> = {
  value: T;
  label: string;
  /** Optional group heading; consecutive options sharing one sit under it. */
  group?: string;
  disabled?: boolean;
  title?: string;
};

const LIST_TEXT = "text-sm";

/** A styled replacement for the native <select>. WebKitGTK on Linux draws the
 * native option popup with the system GTK theme, which under a light theme
 * paints it white while the options inherit our white text — unreadable. This
 * one renders the list in the DOM, so it's the app's own dark surface on every
 * platform. Keyboard: arrows move, Enter/Space pick, Escape closes; the list
 * is fixed-positioned so it escapes scroll containers and modal clipping. */
export default function Select<T extends string | number>({
  value,
  options,
  onChange,
  className = "",
  disabled = false,
  title,
  "aria-label": ariaLabel,
}: {
  value: T;
  options: SelectOption<T>[];
  onChange: (v: T) => void;
  className?: string;
  disabled?: boolean;
  title?: string;
  "aria-label"?: string;
}) {
  const id = useId();
  const btnRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [pos, setPos] = useState<{ top: number; left: number; width: number } | null>(null);

  const selectedIdx = Math.max(
    0,
    options.findIndex((o) => o.value === value)
  );
  const current = options[selectedIdx];

  /** Next enabled index from `from` stepping by `d`, or `from` if none. */
  const step = (from: number, d: 1 | -1) => {
    for (let i = from + d; i >= 0 && i < options.length; i += d) {
      if (!options[i].disabled) return i;
    }
    return from;
  };

  const show = () => {
    if (disabled) return;
    const r = btnRef.current?.getBoundingClientRect();
    if (r) setPos({ top: r.bottom + 4, left: r.left, width: r.width });
    setActive(selectedIdx);
    setOpen(true);
  };
  const hide = () => setOpen(false);
  const pick = (i: number) => {
    const o = options[i];
    if (!o || o.disabled) return;
    hide();
    if (o.value !== value) onChange(o.value);
    btnRef.current?.focus({ preventScroll: true });
  };

  // Flip above the trigger when the list would run off the bottom.
  useLayoutEffect(() => {
    if (!open || !pos || !listRef.current || !btnRef.current) return;
    const lh = listRef.current.offsetHeight;
    const r = btnRef.current.getBoundingClientRect();
    if (r.bottom + 4 + lh > window.innerHeight && r.top - 4 - lh >= 0) {
      setPos({ top: r.top - 4 - lh, left: r.left, width: r.width });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Keep the highlighted option in view while arrowing.
  useEffect(() => {
    if (!open) return;
    listRef.current
      ?.querySelector<HTMLElement>(`[data-idx="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [open, active]);

  // While open: close on outside press, on viewport changes, and on Escape.
  // Escape is caught on window in the capture phase so it runs before a
  // parent dialog's document-level handler and closes only the list.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (btnRef.current?.contains(t) || listRef.current?.contains(t)) return;
      hide();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopImmediatePropagation();
      hide();
      btnRef.current?.focus({ preventScroll: true });
    };
    const onMove = () => hide();
    document.addEventListener("pointerdown", onDown, true);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("resize", onMove);
    window.addEventListener("scroll", onMove, true);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("resize", onMove);
      window.removeEventListener("scroll", onMove, true);
    };
  }, [open]);

  const onKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>) => {
    if (disabled) return;
    switch (e.key) {
      case "ArrowDown":
      case "ArrowUp": {
        e.preventDefault();
        if (!open) {
          show();
          return;
        }
        setActive((a) => step(a, e.key === "ArrowDown" ? 1 : -1));
        return;
      }
      case "Home":
      case "End":
        if (!open) return;
        e.preventDefault();
        setActive(e.key === "Home" ? step(-1, 1) : step(options.length, -1));
        return;
      case "Enter":
      case " ":
        e.preventDefault();
        if (open) pick(active);
        else show();
        return;
      case "Tab":
        if (open) hide();
        return;
    }
  };

  const listId = `${id}-list`;
  return (
    <>
      <button
        ref={btnRef}
        type="button"
        disabled={disabled}
        title={title}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-activedescendant={open ? `${id}-opt-${active}` : undefined}
        onClick={() => (open ? hide() : show())}
        onKeyDown={onKeyDown}
        className={`inline-flex items-center justify-between gap-2 rounded-lg border border-white/10 text-left transition disabled:opacity-40 enabled:hover:border-accent/50 ${
          open ? "border-accent/60" : ""
        } ${className}`}
      >
        <span className="truncate">{current?.label ?? ""}</span>
        <ChevronDownIcon
          className={`h-3.5 w-3.5 shrink-0 text-white/55 transition ${open ? "rotate-180" : ""}`}
        />
      </button>
      {open && pos && (
        <div
          ref={listRef}
          id={listId}
          role="listbox"
          aria-label={ariaLabel}
          style={{ top: pos.top, left: pos.left, minWidth: pos.width }}
          className={`fixed z-[70] max-h-64 overflow-y-auto rounded-lg border border-white/10 bg-ink-600 p-1 ${LIST_TEXT} text-white/85 shadow-card animate-fade-in`}
        >
          {options.map((o, i) => {
            const isSel = i === selectedIdx;
            const isActive = i === active && !o.disabled;
            const heading = o.group && o.group !== options[i - 1]?.group;
            return (
              <div key={`${i}:${String(o.value)}`}>
                {heading && (
                  <div className="px-2.5 pb-1 pt-2 text-[10px] uppercase tracking-wide text-white/40">
                    {o.group}
                  </div>
                )}
                <div
                  id={`${id}-opt-${i}`}
                  data-idx={i}
                  role="option"
                  aria-selected={isSel}
                  aria-disabled={o.disabled || undefined}
                  title={o.title}
                  onMouseDown={(e) => e.preventDefault()}
                  onMouseEnter={() => !o.disabled && setActive(i)}
                  onClick={() => pick(i)}
                  className={`flex items-center justify-between gap-3 whitespace-nowrap rounded-md px-2.5 py-1.5 ${
                    o.disabled ? "cursor-default text-white/35" : "cursor-pointer"
                  } ${isActive ? "bg-accent/25 text-white" : ""} ${
                    isSel && !isActive ? "text-accent-soft" : ""
                  }`}
                >
                  <span>{o.label}</span>
                  {isSel && <span className="text-accent-soft">✓</span>}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </>
  );
}

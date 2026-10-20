import { useEffect, useRef, useState, type ReactNode } from "react";
import { CloseIcon } from "./icons";

/** "May 1, 2023" from a "2023-05-01"; the input back when it isn't one. */
export function dateLabel(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
  if (!m) return iso;
  const d = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3]));
  return d.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

/** The panel every title gets: a drawer over the grid's right edge, with
 * the background art (or the poster, blurred) as a band across its top,
 * the title and facts line drawn over the bottom edge beside the poster,
 * the close button in the corner, then the body in one column — the
 * actions first, then the sections. The grid stays in view and live:
 * another poster swaps the title in, Escape or the close button puts the
 * panel away. The panel scrolls on its own. */
export function DetailPanel({
  label,
  onClose,
  art,
  poster,
  posterPlaceholder,
  wideThumb = false,
  progress = 0,
  kicker,
  title,
  meta,
  actions,
  footer,
  children,
}: {
  label: string;
  onClose: () => void;
  /** Widescreen background art; "" to fall back to the poster. */
  art: string;
  /** The poster (2:3), or an episode's still (16:9, see wideThumb). */
  poster: string;
  posterPlaceholder: ReactNode;
  /** The "poster" is a 16:9 still: it becomes the band when there is no
   *  art, and is not drawn beside the title. */
  wideThumb?: boolean;
  /** 0–100 percent watched, drawn along the band's bottom edge. */
  progress?: number;
  kicker?: ReactNode;
  title: ReactNode;
  /** Joined with " · "; empty parts are dropped. */
  meta?: (ReactNode | false | null | undefined | "")[];
  actions: ReactNode;
  /** Pinned under the scrolling body (a selection's batch actions). */
  footer?: ReactNode;
  children?: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // The panel opens with its main button focused, so Enter acts on it.
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>("[data-autofocus]:not([disabled])")?.focus({ preventScroll: true });
  }, []);

  const parts = (meta ?? []).filter((p) => p !== false && p !== null && p !== undefined && p !== "") as ReactNode[];
  const band = art || (wideThumb ? poster : "");
  const blurred = !art && !wideThumb && !!poster;

  return (
    <div className="pointer-events-none absolute inset-0 z-30" role="presentation">
      <div className="absolute inset-0 bg-black/25 animate-fade-in" aria-hidden />
      <div
        ref={ref}
        data-detail-panel
        role="dialog"
        aria-modal="false"
        aria-label={label}
        className="pointer-events-auto absolute inset-y-0 right-0 flex w-[32rem] max-w-[calc(100vw-14rem)] flex-col border-l border-white/10 bg-ink-800 shadow-[-24px_0_60px_rgba(0,0,0,0.55)] animate-slide-in"
      >
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          <div className="relative">
            {/* The band: the art, shaded towards the panel so the text and
                the body below sit on it; the poster blurred up when there
                is no art. */}
            <div className="absolute inset-0 overflow-hidden bg-ink-750">
              {band ? (
                <img src={band} alt="" draggable={false} className="h-full w-full object-cover" />
              ) : blurred ? (
                <img
                  src={poster}
                  alt=""
                  draggable={false}
                  className="h-full w-full scale-125 object-cover opacity-70 blur-2xl"
                />
              ) : null}
              <div className="absolute inset-0 bg-gradient-to-t from-ink-800 via-ink-800/55 to-ink-800/5" />
              {progress > 0 && progress < 95 && (
                <div className="absolute inset-x-0 bottom-0 h-1 bg-white/20">
                  <div className="h-full bg-accent" style={{ width: `${progress}%` }} />
                </div>
              )}
            </div>

            <div className="relative flex min-h-[16rem] flex-col">
              <div className="flex justify-end px-4 pt-3">
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-full bg-black/45 p-1.5 text-white/70 ring-1 ring-white/15 backdrop-blur transition hover:text-white"
                  title="Close (Escape)"
                  aria-label="Close"
                >
                  <CloseIcon width={16} height={16} />
                </button>
              </div>
              <div className="mt-auto flex items-end gap-4 px-5 pb-4 pt-10">
                {!wideThumb && (
                  <div className="aspect-[2/3] w-24 shrink-0 overflow-hidden rounded-md bg-ink-600 shadow-card ring-1 ring-white/10">
                    {poster ? (
                      <img src={poster} alt="" draggable={false} className="h-full w-full object-cover" />
                    ) : (
                      <div className="flex h-full w-full items-center justify-center text-white/20">
                        {posterPlaceholder}
                      </div>
                    )}
                  </div>
                )}
                <div className="min-w-0 flex-1">
                  {kicker && <div className="mb-0.5 text-xs font-medium text-white/70">{kicker}</div>}
                  <h1 className="text-2xl font-bold leading-tight tracking-tight text-white drop-shadow-sm">
                    {title}
                  </h1>
                  {parts.length > 0 && (
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-1.5 text-xs text-white/70">
                      {parts.map((p, i) => (
                        <span key={i} className="flex items-center gap-x-1.5">
                          {i > 0 && <span className="text-white/30">·</span>}
                          {p}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>

          <div className="px-5 pb-8">
            <div className="space-y-4">{actions}</div>
            <div className="mt-5 space-y-5">{children}</div>
          </div>
        </div>
        {footer}
      </div>
    </div>
  );
}

/** A captioned block of the body. */
export function Section({
  title,
  aside,
  children,
}: {
  title: string;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-3">
        <h2 className="text-[11px] font-semibold uppercase tracking-wider text-white/55">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** A synopsis: readable measure, clipped to a few lines with a "More"
 * toggle when it runs long. */
export function Overview({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  const long = text.length > 420;
  return (
    <div className="max-w-prose">
      <p className={`text-sm leading-relaxed text-white/75 ${long && !open ? "line-clamp-4" : ""}`}>{text}</p>
      {long && (
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="mt-1 text-xs text-accent-soft transition hover:text-white"
        >
          {open ? "Less" : "More"}
        </button>
      )}
    </div>
  );
}

/** Label / value pairs — director, cast, studio. Empty values are
 * dropped; nothing renders when none remain. */
export function Facts({ items }: { items: [string, ReactNode | "" | null | undefined | false][] }) {
  const rows = items.filter(([, v]) => !!v) as [string, ReactNode][];
  if (rows.length === 0) return null;
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-xs">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-white/50">{k}</dt>
          <dd className="min-w-0 break-words text-white/80">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A captioned group of actions — what they do to the title (Watch,
 * Download, Library) — as two columns of equal controls; a lone last one
 * takes the whole row. */
export function ActionGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-white/45">{label}</div>
      <div className="grid grid-cols-2 gap-2 [&>*:last-child:nth-child(odd)]:col-span-2">{children}</div>
    </div>
  );
}

/** The one big button a title is about: Play / Resume, on its own row. */
export function PrimaryAction({
  children,
  icon,
  onClick,
  disabled,
  title,
}: {
  children: ReactNode;
  icon?: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  title?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      data-autofocus
      className="flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-accent text-[15px] font-semibold text-ink-900 transition-colors hover:bg-accent-soft disabled:opacity-50"
    >
      {icon}
      {children}
    </button>
  );
}

/** An action button in a group: full width of its cell, label clipped
 * rather than wrapped. `active` is the pressed look (Favorited, Queued). */
export function Action({
  children,
  icon,
  onClick,
  disabled,
  active = false,
  title,
}: {
  children: ReactNode;
  icon?: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  active?: boolean;
  title?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={`flex h-9 w-full min-w-0 items-center justify-center gap-2 rounded-lg px-3 text-sm font-medium transition-colors disabled:opacity-50 ${
        active
          ? "bg-accent/15 text-accent-soft enabled:hover:bg-accent/25"
          : "bg-white/10 text-white enabled:hover:bg-white/20"
      }`}
    >
      {icon && <span className="shrink-0">{icon}</span>}
      <span className="truncate">{children}</span>
    </button>
  );
}

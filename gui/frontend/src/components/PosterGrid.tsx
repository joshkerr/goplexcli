import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import type { MediaCard } from "../lib/types";
import { PosterCard } from "./PosterCard";

interface Props {
  items: MediaCard[];
  loading: boolean;
  emptyMessage: string;
  onSelect: (media: MediaCard) => void;
  favorites: Set<string>;
  onToggleFavorite: (key: string) => void;
}

// Layout constants (kept in sync with the Tailwind classes below).
const MIN_COL = 160; // target minimum card width in px
const GAP_X = 20; // column gap (gap-x-5)
const GAP_Y = 28; // row gap (gap-y-7)
const LABEL_H = 48; // title + subtitle area beneath each poster
const PAD_X = 32; // px-8
const PAD_Y = 24; // py-6
const OVERSCAN = 2; // extra rows rendered above/below the viewport
const PREFETCH_ROWS = 2; // warm the next rows while the browser is idle
const prefetchedPosters = new Set<string>();

/**
 * A windowed (virtualized) poster grid.
 *
 * Only the rows intersecting the viewport (plus overscan) are mounted, so a
 * 20k-item library renders a few dozen cards instead of 20k. Layout uses a real
 * CSS grid for the visible window with spacer divs above and below — so the
 * browser handles column widths, gaps and row heights (no manual absolute
 * positioning that can drift or overlap). The row height used for the spacers
 * is measured from the rendered grid so scrolling stays accurate.
 *
 * The parent should remount this (via `key`) when the dataset changes so the
 * scroll position resets.
 */
export function PosterGrid({ items, loading, emptyMessage, onSelect, favorites, onToggleFavorite }: Props) {
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ width: 0, height: 0 });
  const [scrollTop, setScrollTop] = useState(0);
  const [measuredRowH, setMeasuredRowH] = useState(0);
  const rafRef = useRef<number | null>(null);

  // Measure the scroll container; re-measure on resize.
  useLayoutEffect(() => {
    if (!scrollEl) return;
    const measure = () =>
      setDims({ width: scrollEl.clientWidth, height: scrollEl.clientHeight });
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(scrollEl);
    return () => ro.disconnect();
  }, [scrollEl]);

  // Throttle scroll updates to one per animation frame. Read scrollTop when the
  // frame fires (not when scheduled) so a fast drag lands on the latest
  // position instead of a frame-stale one.
  const onScroll = () => {
    if (!scrollEl || rafRef.current != null) return;
    rafRef.current = requestAnimationFrame(() => {
      rafRef.current = null;
      setScrollTop(scrollEl.scrollTop);
    });
  };
  useEffect(
    () => () => {
      if (rafRef.current != null) cancelAnimationFrame(rafRef.current);
    },
    []
  );

  const availWidth = Math.max(0, dims.width - PAD_X * 2);
  const cols =
    availWidth > 0
      ? Math.max(1, Math.floor((availWidth + GAP_X) / (MIN_COL + GAP_X)))
      : 1;
  const cardW = (availWidth - (cols - 1) * GAP_X) / cols;
  // Spacer row height: use the value measured from the real grid once we have
  // it, otherwise estimate from the card width so the first frame is close.
  const estRowH = Math.round(cardW * 1.5) + LABEL_H + GAP_Y;
  const rowH = measuredRowH > 0 ? measuredRowH : estRowH;

  const rowCount = Math.ceil(items.length / cols);
  const firstRow = Math.max(
    0,
    Math.floor((scrollTop - PAD_Y) / rowH) - OVERSCAN
  );
  const lastRow = Math.min(
    rowCount - 1,
    Math.floor((scrollTop - PAD_Y + dims.height) / rowH) + OVERSCAN
  );

  const ready = dims.width > 0 && !loading && items.length > 0;
  const startIdx = ready ? firstRow * cols : 0;
  const endIdx = ready ? Math.min(items.length - 1, (lastRow + 1) * cols - 1) : -1;
  const visible = ready ? items.slice(startIdx, endIdx + 1) : [];
  const viewportFirstRow = Math.max(0, Math.floor((scrollTop - PAD_Y) / rowH));
  const viewportLastRow = Math.min(
    rowCount - 1,
    Math.floor((scrollTop - PAD_Y + dims.height) / rowH)
  );

  // Once scrolling settles, warm the posters across the rendered window (plus a
  // couple of rows ahead) via the backend pool. Because the backend transcodes
  // on its own connection pool — not the browser's ~6-per-origin cap — a far
  // jump-scroll fills the whole window in parallel instead of six at a time, and
  // singleflight dedupes against the browser's own image requests. The effect
  // re-runs as the window changes; the 120ms debounce means a fast drag only
  // warms where it lands, not every position swept through.
  useEffect(() => {
    if (!ready) return;
    const from = startIdx;
    const to = Math.min(items.length, endIdx + 1 + cols * PREFETCH_ROWS);
    const id = window.setTimeout(() => {
      const urls: string[] = [];
      for (let i = from; i < to; i++) {
        const src = items[i]?.thumbURL;
        if (!src || prefetchedPosters.has(src)) continue;
        prefetchedPosters.add(src);
        urls.push(src);
      }
      if (urls.length > 0) api.warmPosters(urls).catch(() => {});
    }, 120);
    return () => window.clearTimeout(id);
  }, [ready, items, startIdx, endIdx, cols]);

  // Spacer heights above/below the rendered window.
  const topPad = PAD_Y + firstRow * rowH;
  const bottomPad = Math.max(0, (rowCount - 1 - lastRow) * rowH) + PAD_Y;

  // Measure the actual rendered row height (poster + label + row gap) so the
  // spacers — and therefore the scrollbar — stay accurate. Guarded so it only
  // updates on a real change (prevents an update loop).
  useLayoutEffect(() => {
    const el = gridRef.current;
    if (!el || el.children.length === 0) return;
    const cardH = (el.children[0] as HTMLElement).offsetHeight;
    const stride = cardH + GAP_Y;
    setMeasuredRowH((prev) => (Math.abs(prev - stride) > 0.5 ? stride : prev));
  });

  return (
    <div ref={setScrollEl} onScroll={onScroll} className="h-full overflow-y-auto">
      {loading ? (
        <div className="px-8 py-6">
          <div className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-x-5 gap-y-7">
            {Array.from({ length: 24 }).map((_, i) => (
              <div key={i} className="flex flex-col">
                <div className="aspect-[2/3] w-full animate-pulse rounded-xl bg-ink-600/60" />
                <div className="mt-2 h-3 w-3/4 animate-pulse rounded bg-ink-600/60" />
              </div>
            ))}
          </div>
        </div>
      ) : items.length === 0 ? (
        <div className="flex h-full flex-col items-center justify-center px-8 text-center">
          <div className="text-base font-medium text-white/60">
            {emptyMessage}
          </div>
        </div>
      ) : (
        <>
          <div style={{ height: topPad }} />
          <div
            ref={gridRef}
            className="grid px-8"
            style={{
              gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
              columnGap: GAP_X,
              rowGap: GAP_Y,
            }}
          >
            {visible.map((item, i) => (
              <PosterCard
                key={item.key}
                media={item}
                onClick={() => onSelect(item)}
                favorite={favorites.has(item.key)}
                onToggleFavorite={() => onToggleFavorite(item.key)}
                priority={
                  Math.floor((startIdx + i) / cols) >= viewportFirstRow &&
                  Math.floor((startIdx + i) / cols) <= viewportLastRow
                }
              />
            ))}
          </div>
          <div style={{ height: bottomPad }} />
        </>
      )}
    </div>
  );
}

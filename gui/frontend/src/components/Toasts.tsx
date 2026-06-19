export interface Toast {
  id: number;
  message: string;
  kind: "info" | "error";
}

interface Props {
  toasts: Toast[];
  onDismiss: (id: number) => void;
}

export function Toasts({ toasts, onDismiss }: Props) {
  return (
    <div className="pointer-events-none fixed bottom-5 right-5 z-[60] flex w-80 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          onClick={() => onDismiss(t.id)}
          className={`pointer-events-auto animate-fade-in cursor-pointer rounded-xl border px-4 py-3 text-sm shadow-card backdrop-blur ${
            t.kind === "error"
              ? "border-red-500/30 bg-red-950/80 text-red-200"
              : "border-white/10 bg-ink-700/90 text-white/90"
          }`}
        >
          {t.message}
        </div>
      ))}
    </div>
  );
}

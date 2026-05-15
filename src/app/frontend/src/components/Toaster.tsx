import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import {
  ToastContext,
  type Toast,
  type ToastKind,
} from "@/contexts/ToastContext";

const AUTO_DISMISS_MS = 3000;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const idRef = useRef(0);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const push = useCallback((toast: Omit<Toast, "id">) => {
    idRef.current += 1;
    const id = idRef.current;
    setToasts((prev) => [...prev, { ...toast, id }]);
  }, []);

  return (
    <ToastContext.Provider value={{ push }}>
      {children}
      <Toaster toasts={toasts} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

function Toaster({
  toasts,
  onDismiss,
}: {
  toasts: Toast[];
  onDismiss: (id: number) => void;
}) {
  useEffect(() => {
    const timers = toasts
      .filter((t) => t.kind === "success" || t.kind === "info")
      .map((t) => window.setTimeout(() => onDismiss(t.id), AUTO_DISMISS_MS));
    return () => {
      timers.forEach((id) => window.clearTimeout(id));
    };
  }, [toasts, onDismiss]);

  if (typeof document === "undefined") return null;

  return createPortal(
    <div
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex max-w-full flex-col gap-2"
      aria-live="polite"
      aria-atomic="false"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          role={t.kind === "error" || t.kind === "warning" ? "alert" : "status"}
          className={`pointer-events-auto flex w-96 max-w-[calc(100vw-2rem)] items-start gap-3 p-4 ${kindClass(t.kind)}`}
        >
          <p className="flex-1 text-sm">{t.message}</p>
          {t.action ? (
            t.action.href ? (
              <a
                href={t.action.href}
                className="text-sm font-semibold underline"
              >
                {t.action.label}
              </a>
            ) : (
              <button
                type="button"
                onClick={() => {
                  t.action?.onClick?.();
                  onDismiss(t.id);
                }}
                className="text-sm font-semibold underline"
              >
                {t.action.label}
              </button>
            )
          ) : null}
          <button
            type="button"
            onClick={() => onDismiss(t.id)}
            aria-label="Dismiss notification"
            className="text-lg leading-none text-lw-ink-500 hover:text-lw-ink-900"
          >
            ×
          </button>
        </div>
      ))}
    </div>,
    document.body,
  );
}

function kindClass(kind: ToastKind): string {
  switch (kind) {
    case "error":
      return "tamoss-callout-danger";
    case "warning":
      return "tamoss-callout-warn";
    case "success":
      return "tamoss-callout-info border-emerald-200 bg-emerald-50";
    case "info":
    default:
      return "tamoss-callout-info";
  }
}

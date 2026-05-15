import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";

export type ConfirmActionVariant = "danger" | "warning" | "info";

interface ConfirmActionProps {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: ConfirmActionVariant;
  busy?: boolean;
  busyLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmAction({
  open,
  title,
  description,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  variant = "danger",
  busy = false,
  busyLabel = "Working...",
  onConfirm,
  onCancel,
}: ConfirmActionProps) {
  const confirmRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) {
        onCancel();
      }
    };
    document.addEventListener("keydown", handler);
    const t = window.setTimeout(() => confirmRef.current?.focus(), 0);
    return () => {
      document.removeEventListener("keydown", handler);
      window.clearTimeout(t);
    };
  }, [open, busy, onCancel]);

  if (!open || typeof document === "undefined") return null;

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="confirm-action-title"
      onClick={(event) => {
        if (event.target === event.currentTarget && !busy) {
          onCancel();
        }
      }}
    >
      <div className="tamoss-panel w-full max-w-md rounded-2xl p-6">
        <h2
          id="confirm-action-title"
          className="text-base font-semibold text-lw-ink-900"
        >
          {title}
        </h2>
        {description && (
          <div className="mt-2 text-sm leading-6 text-lw-ink-700">
            {description}
          </div>
        )}
        <div className="mt-5 flex flex-wrap justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded-xl border border-lw-ink-100 bg-white px-4 py-2 text-sm font-medium text-lw-ink-800 hover:bg-lw-ink-50 disabled:opacity-50"
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmRef}
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className={confirmClass(variant)}
          >
            {busy ? busyLabel : confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function confirmClass(variant: ConfirmActionVariant): string {
  const base =
    "rounded-xl px-4 py-2 text-sm font-medium text-white disabled:opacity-50";
  switch (variant) {
    case "warning":
      return `${base} bg-amber-600 hover:bg-amber-700`;
    case "info":
      return `${base} bg-tams-700 hover:bg-tams-800`;
    case "danger":
    default:
      return `${base} bg-red-700 hover:bg-red-800`;
  }
}

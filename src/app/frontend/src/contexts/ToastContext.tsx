import { createContext } from "react";

export type ToastKind = "success" | "error" | "info" | "warning";

export interface ToastAction {
  label: string;
  href?: string;
  onClick?: () => void;
}

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
  action?: ToastAction;
}

export interface ToastContextValue {
  push(toast: Omit<Toast, "id">): void;
}

export const ToastContext = createContext<ToastContextValue | null>(null);

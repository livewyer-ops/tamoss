import { AlertCircle, LoaderCircle } from "lucide-react";
import { type ButtonHTMLAttributes, forwardRef, type ReactNode } from "react";
import styles from "./Surface.module.css";

export function Page({ children }: { children: ReactNode }) {
  return <div className={styles.page}>{children}</div>;
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className={styles.pageHeader}>
      <div>
        <h1 className={styles.title}>{title}</h1>
        {description ? (
          <p className={styles.description}>{description}</p>
        ) : null}
      </div>
      {actions ? <div className={styles.actions}>{actions}</div> : null}
    </header>
  );
}

export function Panel({
  title,
  actions,
  children,
  className = "",
}: {
  title?: string;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`${styles.panel} ${className}`}>
      {title || actions ? (
        <header className={styles.panelHeader}>
          {title ? <h2 className={styles.panelTitle}>{title}</h2> : <span />}
          {actions}
        </header>
      ) : null}
      {children}
    </section>
  );
}

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & {
    variant?: "secondary" | "primary" | "danger";
  }
>(function Button({ variant = "secondary", className = "", ...props }, ref) {
  return (
    <button
      {...props}
      ref={ref}
      className={`${styles.button} ${variant === "primary" ? styles.primary : ""} ${variant === "danger" ? styles.danger : ""} ${className}`}
    />
  );
});

export function StatusBadge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "error" | "info";
}) {
  const toneClass = tone === "neutral" ? "" : styles[tone];
  return <span className={`${styles.badge} ${toneClass}`}>{children}</span>;
}

export function QueryMessage({
  loading,
  error,
  empty,
  onRetry,
}: {
  loading?: boolean;
  error?: Error | null;
  empty?: { title: string; description?: string };
  onRetry?: () => void;
}) {
  if (loading) {
    return (
      <div className={styles.message} role="status">
        <div>
          <LoaderCircle size={20} aria-hidden="true" />
          <strong>Loading</strong>
          <span>Requesting the latest service data.</span>
        </div>
      </div>
    );
  }
  if (error) {
    return (
      <div className={`${styles.message} ${styles.errorMessage}`} role="alert">
        <div>
          <AlertCircle size={20} aria-hidden="true" />
          <strong>Request failed</strong>
          <span>{error.message}</span>
          {onRetry ? (
            <div style={{ marginTop: 12 }}>
              <Button type="button" onClick={onRetry}>
                Retry
              </Button>
            </div>
          ) : null}
        </div>
      </div>
    );
  }
  if (empty) {
    return (
      <div className={styles.empty}>
        <div>
          <strong>{empty.title}</strong>
          {empty.description ? <span>{empty.description}</span> : null}
        </div>
      </div>
    );
  }
  return null;
}

export { styles as surfaceStyles };

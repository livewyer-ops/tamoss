import { Link } from "react-router-dom";

interface ErrorMessageProps {
  message: string;
  onRetry?: () => void;
  title?: string;
  links?: Array<{ label: string; to: string }>;
}

export default function ErrorMessage({
  message,
  onRetry,
  title = "Error",
  links = [],
}: ErrorMessageProps) {
  return (
    <div
      className="rounded-2xl border border-rose-200 bg-rose-50 p-5"
      role="alert"
    >
      <div className="flex items-start gap-3">
        <svg
          className="mt-0.5 h-5 w-5 flex-shrink-0 text-rose-500"
          fill="none"
          viewBox="0 0 24 24"
          strokeWidth={1.5}
          stroke="currentColor"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z"
          />
        </svg>
        <div className="flex-1">
          <p className="text-sm font-semibold uppercase tracking-[0.14em] text-rose-800">
            {title}
          </p>
          <p className="mt-1 text-sm text-rose-700">{message}</p>
          {links.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-2">
              {links.map((link) => (
                <Link
                  key={`${link.to}-${link.label}`}
                  to={link.to}
                  className="rounded-xl border border-rose-200 bg-white px-3 py-1.5 text-sm font-medium text-rose-700 hover:bg-rose-100"
                >
                  {link.label}
                </Link>
              ))}
            </div>
          )}
        </div>
        {onRetry && (
          <button
            onClick={onRetry}
            className="rounded-xl bg-rose-100 px-3 py-1.5 text-sm font-medium text-rose-700 hover:bg-rose-200"
          >
            Retry
          </button>
        )}
      </div>
    </div>
  );
}

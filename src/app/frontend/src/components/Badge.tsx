const variants = {
  default: "border border-lw-ink-100 bg-white text-lw-ink-700",
  primary: "border border-tams-200 bg-tams-50 text-tams-900",
  success: "border border-emerald-200 bg-emerald-50 text-emerald-800",
  warning: "border border-amber-200 bg-amber-50 text-amber-800",
  danger: "border border-rose-200 bg-rose-50 text-rose-800",
  info: "border border-sky-200 bg-sky-50 text-sky-800",
} as const;

interface BadgeProps {
  children: React.ReactNode;
  variant?: keyof typeof variants;
  className?: string;
}

export default function Badge({
  children,
  variant = "default",
  className = "",
}: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-1 text-[0.7rem] font-semibold uppercase tracking-[0.14em] ${variants[variant]} ${className}`}
    >
      {children}
    </span>
  );
}

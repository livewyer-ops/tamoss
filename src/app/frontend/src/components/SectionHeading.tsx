export default function SectionHeading({
  eyebrow,
  title,
  description,
}: {
  eyebrow?: string;
  title: string;
  description?: string;
}) {
  return (
    <div className="mb-5">
      {eyebrow && (
        <p className="tamoss-eyebrow mb-1.5 text-[0.68rem] font-semibold uppercase">
          {eyebrow}
        </p>
      )}
      <h2 className="text-xl font-semibold text-lw-ink-900">{title}</h2>
      {description && (
        <p className="mt-1.5 max-w-3xl text-sm leading-6 text-lw-ink-500">
          {description}
        </p>
      )}
    </div>
  );
}

type TamossMarkProps = {
  className?: string;
};

export default function TamossMark({ className = "" }: TamossMarkProps) {
  const classes = ["block", className].filter(Boolean).join(" ");

  return (
    <img
      src="/tamoss-icon.png"
      alt=""
      aria-hidden="true"
      className={classes}
      draggable={false}
    />
  );
}

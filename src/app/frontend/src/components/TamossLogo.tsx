type TamossLogoProps = {
  className?: string;
};

export default function TamossLogo({ className = "" }: TamossLogoProps) {
  const classes = ["block", className].filter(Boolean).join(" ");

  return (
    <img
      src="/tamoss-logo-transparent.png"
      alt="TAMOSS"
      className={classes}
      draggable={false}
    />
  );
}

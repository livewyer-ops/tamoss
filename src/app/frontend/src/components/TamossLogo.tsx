type TamossLogoProps = {
  className?: string;
};

export default function TamossLogo({ className = "" }: TamossLogoProps) {
  return (
    <img
      src="/tamoss-logo-ibc.png"
      alt="TAMOSS"
      className={className}
      width={2284}
      height={1040}
      draggable={false}
    />
  );
}

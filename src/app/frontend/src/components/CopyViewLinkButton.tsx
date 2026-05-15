import { useMemo } from "react";
import { useLocation } from "react-router-dom";
import CopyButton from "@/components/CopyButton";

export default function CopyViewLinkButton({
  label = "Copy View",
}: {
  label?: string;
}) {
  const location = useLocation();

  const href = useMemo(() => {
    if (typeof window === "undefined") {
      return `${location.pathname}${location.search}${location.hash}`;
    }
    return `${window.location.origin}${location.pathname}${location.search}${location.hash}`;
  }, [location]);

  return <CopyButton text={href} label={label} />;
}

import { useEffect } from "react";

export function usePageTitle(pageTitle: string) {
  useEffect(() => {
    document.title = `${pageTitle} · TAMOSS`;
  }, [pageTitle]);
}

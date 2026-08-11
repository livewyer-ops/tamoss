import { ChevronLeft, ChevronRight, RefreshCw } from "lucide-react";
import { Button, surfaceStyles } from "@/components/Surface";

export function CatalogPager({
  itemCount,
  hasPrevious,
  hasNext,
  loading,
  onPrevious,
  onNext,
  onRefresh,
}: {
  itemCount: number;
  hasPrevious: boolean;
  hasNext: boolean;
  loading: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onRefresh: () => void;
}) {
  return (
    <footer className={surfaceStyles.pager}>
      <span>
        {loading ? "Loading page" : `${itemCount} items on this page`}
      </span>
      <div className={surfaceStyles.toolbar}>
        <Button
          type="button"
          onClick={onRefresh}
          disabled={loading}
          title="Refresh page"
        >
          <RefreshCw size={14} aria-hidden="true" /> Refresh
        </Button>
        <Button
          type="button"
          onClick={onPrevious}
          disabled={loading || !hasPrevious}
        >
          <ChevronLeft size={14} aria-hidden="true" /> Previous
        </Button>
        <Button type="button" onClick={onNext} disabled={loading || !hasNext}>
          Next <ChevronRight size={14} aria-hidden="true" />
        </Button>
      </div>
    </footer>
  );
}

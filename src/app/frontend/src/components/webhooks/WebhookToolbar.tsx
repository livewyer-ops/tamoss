import CopyViewLinkButton from "@/components/CopyViewLinkButton";

interface WebhookToolbarProps {
  filter: string;
  statusFilter: string;
  compactMode: boolean;
  creating: boolean;
  onFilterChange: (value: string) => void;
  onStatusFilterChange: (value: string) => void;
  onRefresh: () => void;
  onToggleCompactMode: () => void;
  onToggleCreating: () => void;
}

export default function WebhookToolbar({
  filter,
  statusFilter,
  compactMode,
  creating,
  onFilterChange,
  onStatusFilterChange,
  onRefresh,
  onToggleCompactMode,
  onToggleCreating,
}: WebhookToolbarProps) {
  return (
    <div className="flex gap-2">
      <input
        type="search"
        value={filter}
        onChange={(event) => onFilterChange(event.target.value)}
        placeholder="Filter webhooks..."
        className="rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
      />
      <select
        value={statusFilter}
        onChange={(event) => onStatusFilterChange(event.target.value)}
        className="rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
      >
        <option value="">All statuses</option>
        <option value="created">Created</option>
        <option value="started">Started</option>
        <option value="disabled">Disabled</option>
        <option value="error">Error</option>
      </select>
      <button
        onClick={onRefresh}
        className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
      >
        Refresh
      </button>
      <CopyViewLinkButton />
      <button
        onClick={onToggleCompactMode}
        className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
      >
        {compactMode ? "Comfortable rows" : "Compact rows"}
      </button>
      <button
        onClick={onToggleCreating}
        className="rounded-lg bg-tams-600 px-3 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700"
      >
        {creating ? "Close" : "New webhook"}
      </button>
    </div>
  );
}

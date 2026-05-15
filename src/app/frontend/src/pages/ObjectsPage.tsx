import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import EmptyState from "@/components/EmptyState";
import { usePageTitle } from "@/hooks/usePageTitle";

export default function ObjectsPage() {
  usePageTitle("Objects");
  const [objectId, setObjectId] = useState("");
  const navigate = useNavigate();

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = objectId.trim();
    if (!trimmed) return;
    navigate(`/objects/${encodeURIComponent(trimmed)}`);
  }

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6">
        <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">Objects</h1>
        <p className="mt-1 text-sm text-gray-500">
          Look up a media object directly by object ID
        </p>
      </div>

      <div className="tamoss-panel rounded-2xl p-6">
        <form
          className="flex flex-col gap-3 sm:flex-row"
          onSubmit={handleSubmit}
        >
          <input
            type="text"
            value={objectId}
            onChange={(event) => setObjectId(event.target.value)}
            placeholder="Enter object ID"
            className="flex-1 rounded-lg border border-gray-300 px-4 py-2.5 font-mono text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
          />
          <button
            type="submit"
            disabled={!objectId.trim()}
            className="rounded-lg bg-tams-600 px-4 py-2.5 text-sm font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
          >
            Inspect object
          </button>
        </form>
      </div>

      <div className="mt-8 tamoss-panel rounded-2xl p-6">
        <EmptyState
          title="Object lookup"
          description="TAMOSS exposes object details by ID rather than through a list endpoint. Paste an object ID from a flow segment or webhook payload to inspect it here."
          icon="M3.75 3v11.25A2.25 2.25 0 006 16.5h12M3.75 3h10.5A2.25 2.25 0 0116.5 5.25v10.5m-12.75-12.75h10.5A2.25 2.25 0 0116.5 5.25m0 10.5H18a2.25 2.25 0 012.25 2.25V21m-3.75-5.25h-9A2.25 2.25 0 005.25 18v.75A2.25 2.25 0 007.5 21h9A2.25 2.25 0 0018.75 18v-.75A2.25 2.25 0 0016.5 15.75z"
        />
      </div>
    </div>
  );
}

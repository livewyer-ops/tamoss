import { useState, useCallback, useRef } from "react";
import { Link } from "react-router-dom";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import {
  useIngestSession,
  type IngestFile,
  type IngestFileStatus,
  type SourceDraft,
} from "@/hooks/useIngestSession";
import { usePageTitle } from "@/hooks/usePageTitle";
import Badge from "@/components/Badge";
import ErrorMessage from "@/components/ErrorMessage";

const statusConfig: Record<
  IngestFileStatus,
  {
    label: string;
    variant: "default" | "primary" | "success" | "warning" | "danger" | "info";
  }
> = {
  pending: { label: "Pending", variant: "default" },
  probing: { label: "Probing", variant: "info" },
  segmenting: { label: "Segmenting", variant: "info" },
  uploading: { label: "Uploading", variant: "primary" },
  registering: { label: "Registering", variant: "warning" },
  done: { label: "Done", variant: "success" },
  error: { label: "Error", variant: "danger" },
};

type SourceMode = "existing" | "create";

const SOURCE_FORMAT_OPTIONS = [
  {
    value: "urn:x-nmos:format:multi",
    label: "Multi",
    hint: "Use when an upload may create both video and audio flows.",
  },
  {
    value: "urn:x-nmos:format:video",
    label: "Video",
    hint: "Use for video-only sources.",
  },
  {
    value: "urn:x-nmos:format:audio",
    label: "Audio",
    hint: "Use for audio-only sources.",
  },
] as const;
type SourceFormat = (typeof SOURCE_FORMAT_OPTIONS)[number]["value"];

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export default function IngestPage() {
  usePageTitle("Ingest");
  const api = useApi();
  const {
    session,
    addFiles,
    removeFile,
    setSourceId,
    setSegmentDuration,
    startIngest,
    reset,
  } = useIngestSession();
  const sources = useApiQuery(() => api.getSources({ limit: "100" }), [api]);

  const [dragOver, setDragOver] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sourceMode, setSourceMode] = useState<SourceMode>("existing");
  const [newSourceLabel, setNewSourceLabel] = useState("");
  const [newSourceDescription, setNewSourceDescription] = useState("");
  const [newSourceFormat, setNewSourceFormat] = useState<SourceFormat>(
    SOURCE_FORMAT_OPTIONS[0].value,
  );
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragOver(false);
      const files = Array.from(e.dataTransfer.files).filter(
        (f) => f.type.startsWith("video/") || f.type.startsWith("audio/"),
      );
      if (files.length > 0) addFiles(files);
    },
    [addFiles],
  );

  const handleFileInput = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const files = Array.from(e.target.files ?? []);
      if (files.length > 0) addFiles(files);
      e.target.value = "";
    },
    [addFiles],
  );

  const handleStart = useCallback(async () => {
    setError(null);
    try {
      let source: string | SourceDraft | null = session.sourceId;
      if (sourceMode === "create") {
        const label = newSourceLabel.trim();
        if (!label) {
          setError("Source label is required before ingest can start.");
          return;
        }
        source = {
          id: crypto.randomUUID(),
          format: newSourceFormat,
          label,
          description: newSourceDescription.trim() || undefined,
        };
        setSourceId(source.id);
      }
      if (!source) {
        setError("Select or create a source before starting ingest.");
        return;
      }
      await startIngest(source);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ingest failed");
    }
  }, [
    newSourceDescription,
    newSourceFormat,
    newSourceLabel,
    session.sourceId,
    setSourceId,
    sourceMode,
    startIngest,
  ]);

  const allDone =
    session.files.length > 0 && session.files.every((f) => f.status === "done");
  const hasPending = session.files.some((f) => f.status === "pending");
  const hasSourceForIngest =
    sourceMode === "create"
      ? newSourceLabel.trim().length > 0 && newSourceFormat.length > 0
      : Boolean(session.sourceId);

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:px-8">
      <div className="mb-6">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-bold text-gray-900">Ingest Media</h1>
          <Badge variant="warning">Preview</Badge>
        </div>
        <p className="mt-1 text-sm text-gray-500">
          Upload video and audio files through a preview addon workflow. They
          will be segmented and registered with the TAMOSS API.
        </p>
      </div>

      {/* Settings */}
      <div className="mb-6 grid gap-4 sm:grid-cols-2">
        <div>
          <span className="block text-sm font-medium text-gray-700">
            Source for new flows <span className="text-red-500">*</span>
          </span>
          <div className="mt-1 flex rounded-lg bg-gray-100 p-1">
            <button
              type="button"
              onClick={() => setSourceMode("existing")}
              disabled={session.running}
              className={`flex-1 rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-50 ${
                sourceMode === "existing"
                  ? "bg-white text-gray-900 shadow-sm"
                  : "text-gray-500 hover:text-gray-700"
              }`}
            >
              Existing source
            </button>
            <button
              type="button"
              onClick={() => {
                setSourceMode("create");
                setSourceId(null);
              }}
              disabled={session.running}
              className={`flex-1 rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-50 ${
                sourceMode === "create"
                  ? "bg-white text-gray-900 shadow-sm"
                  : "text-gray-500 hover:text-gray-700"
              }`}
            >
              Create source
            </button>
          </div>

          {sourceMode === "existing" ? (
            <select
              id="source-select"
              value={session.sourceId ?? ""}
              onChange={(e) => setSourceId(e.target.value || null)}
              disabled={session.running}
              className="mt-3 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-gray-50"
            >
              <option value="">Select a source...</option>
              {sources.data?.data.map((source) => (
                <option key={source.id} value={source.id}>
                  {source.label || source.id}
                </option>
              ))}
            </select>
          ) : (
            <div className="mt-3 space-y-3">
              <div>
                <label
                  htmlFor="new-source-label"
                  className="block text-xs font-medium text-gray-600"
                >
                  New source label <span className="text-red-500">*</span>
                </label>
                <input
                  id="new-source-label"
                  type="text"
                  value={newSourceLabel}
                  onChange={(e) => setNewSourceLabel(e.target.value)}
                  disabled={session.running}
                  placeholder="e.g. Uploaded camera source"
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-gray-50"
                />
              </div>
              <div>
                <label
                  htmlFor="new-source-format"
                  className="block text-xs font-medium text-gray-600"
                >
                  Source format <span className="text-red-500">*</span>
                </label>
                <select
                  id="new-source-format"
                  value={newSourceFormat}
                  onChange={(e) =>
                    setNewSourceFormat(e.target.value as SourceFormat)
                  }
                  disabled={session.running}
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-gray-50"
                >
                  {SOURCE_FORMAT_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
                <p className="mt-1 text-xs text-gray-500">
                  {
                    SOURCE_FORMAT_OPTIONS.find(
                      (option) => option.value === newSourceFormat,
                    )?.hint
                  }
                </p>
              </div>
              <div>
                <label
                  htmlFor="new-source-description"
                  className="block text-xs font-medium text-gray-600"
                >
                  Description{" "}
                  <span className="font-normal text-gray-400">(optional)</span>
                </label>
                <textarea
                  id="new-source-description"
                  value={newSourceDescription}
                  onChange={(e) => setNewSourceDescription(e.target.value)}
                  disabled={session.running}
                  rows={2}
                  placeholder="Optional context for this ingest source"
                  className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-gray-50"
                />
              </div>
            </div>
          )}
          <p className="mt-1 text-xs text-gray-500">
            Ingest creates new flow resources from uploaded media. TAMS flows
            must be linked to a source, so choose an existing source or create
            one for this upload.
          </p>
        </div>
        <div>
          <label
            htmlFor="seg-duration"
            className="block text-sm font-medium text-gray-700"
          >
            Segment Duration (seconds)
          </label>
          <input
            id="seg-duration"
            type="number"
            min={1}
            value={session.segmentDuration}
            onChange={(e) =>
              setSegmentDuration(parseInt(e.target.value, 10) || 6)
            }
            disabled={session.running}
            className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-gray-50"
          />
        </div>
      </div>

      {/* Drop zone */}
      {!session.running && (
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
          onClick={() => fileInputRef.current?.click()}
          className={`mb-6 flex cursor-pointer flex-col items-center justify-center rounded-lg border-2 border-dashed px-6 py-10 transition-colors ${
            dragOver
              ? "border-tams-400 bg-tams-50"
              : "border-gray-300 bg-white hover:border-gray-400"
          }`}
        >
          <svg
            className="mb-3 h-10 w-10 text-gray-400"
            fill="none"
            viewBox="0 0 24 24"
            strokeWidth={1.5}
            stroke="currentColor"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-13.5-9L12 3m0 0l4.5 4.5M12 3v13.5"
            />
          </svg>
          <p className="text-sm text-gray-600">
            <span className="font-medium text-tams-600">Click to upload</span>{" "}
            or drag and drop
          </p>
          <p className="mt-1 text-xs text-gray-500">Video and audio files</p>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            accept="video/*,audio/*"
            onChange={handleFileInput}
            className="hidden"
          />
        </div>
      )}

      {/* File list */}
      {session.files.length > 0 && (
        <div className="mb-6 overflow-hidden rounded-lg border border-gray-200 bg-white">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                  File
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                  Size
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                  Tracks
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                  Status
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                  Progress
                </th>
                {!session.running && (
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">
                    Actions
                  </th>
                )}
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {session.files.map((f: IngestFile) => (
                <FileRow
                  key={f.id}
                  file={f}
                  running={session.running}
                  onRemove={() => removeFile(f.id)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {(session.sourceId ||
        session.files.some(
          (file) => file.videoFlowId || file.audioFlowId || file.multiFlowId,
        )) && (
        <div className="mb-6 tamoss-panel rounded-2xl p-6">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Created resources
          </h2>
          {session.sourceId && (
            <div className="mb-4 rounded-lg border border-gray-200 p-4">
              <p className="text-sm font-medium text-gray-700">Source</p>
              <div className="mt-2 flex items-center gap-2">
                <code className="text-xs text-gray-500">
                  {session.sourceId}
                </code>
                <Link
                  to={`/sources/${session.sourceId}`}
                  className="text-sm font-medium text-tams-600 hover:text-tams-700"
                >
                  Open source
                </Link>
              </div>
            </div>
          )}

          <div className="space-y-3">
            {session.files
              .filter(
                (file) =>
                  file.videoFlowId || file.audioFlowId || file.multiFlowId,
              )
              .map((file) => (
                <div
                  key={file.id}
                  className="rounded-lg border border-gray-200 p-4"
                >
                  <p className="text-sm font-medium text-gray-700">
                    {file.file.name}
                  </p>
                  <div className="mt-3 flex flex-wrap gap-2">
                    {file.videoFlowId && (
                      <Link
                        to={`/flows/${file.videoFlowId}`}
                        className="rounded-full bg-blue-50 px-3 py-1 text-xs font-medium text-blue-700 hover:bg-blue-100"
                      >
                        Video flow
                      </Link>
                    )}
                    {file.audioFlowId && (
                      <Link
                        to={`/flows/${file.audioFlowId}`}
                        className="rounded-full bg-green-50 px-3 py-1 text-xs font-medium text-green-700 hover:bg-green-100"
                      >
                        Audio flow
                      </Link>
                    )}
                    {file.multiFlowId && (
                      <Link
                        to={`/flows/${file.multiFlowId}`}
                        className="rounded-full bg-purple-50 px-3 py-1 text-xs font-medium text-purple-700 hover:bg-purple-100"
                      >
                        Multi flow
                      </Link>
                    )}
                  </div>
                </div>
              ))}
          </div>
        </div>
      )}

      {/* Error */}
      {error && (
        <div className="mb-4">
          <ErrorMessage title="Ingest failed" message={error} />
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-3">
        {!session.running && !allDone && (
          <button
            onClick={handleStart}
            disabled={
              !hasPending || session.files.length === 0 || !hasSourceForIngest
            }
            className="rounded-md bg-tams-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700 focus:outline-none focus:ring-2 focus:ring-tams-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {sourceMode === "create"
              ? "Create Source & Start Ingest"
              : "Start Ingest"}
          </button>
        )}
        {session.running && (
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <div className="h-4 w-4 animate-spin rounded-full border-2 border-gray-200 border-t-tams-600" />
            Processing...
          </div>
        )}
        {allDone && session.sourceId && (
          <div className="flex items-center gap-3">
            <Badge variant="success">Ingest Complete</Badge>
            <Link
              to={`/sources/${session.sourceId}`}
              className="text-sm font-medium text-tams-600 hover:text-tams-700"
            >
              Inspect Source &rarr;
            </Link>
          </div>
        )}
        {(allDone ||
          (!session.running &&
            session.files.some((f) => f.status !== "pending"))) && (
          <button
            onClick={reset}
            className="rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
          >
            Reset
          </button>
        )}
      </div>
    </div>
  );
}

function FileRow({
  file,
  running,
  onRemove,
}: {
  file: IngestFile;
  running: boolean;
  onRemove: () => void;
}) {
  const cfg = statusConfig[file.status];

  return (
    <tr>
      <td className="whitespace-nowrap px-4 py-3 text-sm font-medium text-gray-900">
        {file.file.name}
      </td>
      <td className="whitespace-nowrap px-4 py-3 text-sm text-gray-500">
        {formatSize(file.file.size)}
      </td>
      <td className="whitespace-nowrap px-4 py-3 text-sm">
        <div className="flex gap-1">
          {file.tracks.hasVideo && <Badge variant="info">Video</Badge>}
          {file.tracks.hasAudio && <Badge variant="info">Audio</Badge>}
          {!file.tracks.hasVideo &&
            !file.tracks.hasAudio &&
            file.status === "pending" && (
              <span className="text-gray-400">--</span>
            )}
        </div>
      </td>
      <td className="whitespace-nowrap px-4 py-3 text-sm">
        <Badge variant={cfg.variant}>{cfg.label}</Badge>
        {file.error && (
          <span className="ml-2 text-xs text-red-500" title={file.error}>
            {file.error.length > 40
              ? file.error.slice(0, 40) + "..."
              : file.error}
          </span>
        )}
        {(file.videoFlowId || file.audioFlowId || file.multiFlowId) && (
          <div className="mt-2 flex flex-wrap gap-1">
            {file.videoFlowId && (
              <Link
                to={`/flows/${file.videoFlowId}`}
                className="text-xs text-tams-600 hover:text-tams-700"
              >
                Video flow
              </Link>
            )}
            {file.audioFlowId && (
              <Link
                to={`/flows/${file.audioFlowId}`}
                className="text-xs text-tams-600 hover:text-tams-700"
              >
                Audio flow
              </Link>
            )}
            {file.multiFlowId && (
              <Link
                to={`/flows/${file.multiFlowId}`}
                className="text-xs text-tams-600 hover:text-tams-700"
              >
                Multi flow
              </Link>
            )}
          </div>
        )}
      </td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <div className="h-2 w-24 overflow-hidden rounded-full bg-gray-200">
            <div
              className="h-full rounded-full bg-tams-600 transition-all"
              style={{ width: `${file.progress}%` }}
            />
          </div>
          <span className="text-xs text-gray-500">{file.progress}%</span>
        </div>
      </td>
      {!running && (
        <td className="whitespace-nowrap px-4 py-3 text-right text-sm">
          {file.status === "pending" && (
            <button
              onClick={onRemove}
              className="text-red-500 hover:text-red-700"
            >
              Remove
            </button>
          )}
        </td>
      )}
    </tr>
  );
}

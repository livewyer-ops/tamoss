import { PackageSearch } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useNavigate } from "react-router";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  surfaceStyles,
} from "@/components/Surface";
import { usePageTitle } from "@/hooks/usePageTitle";

export default function ObjectsPage() {
  usePageTitle("Object lookup");
  const [id, setId] = useState("");
  const navigate = useNavigate();
  function submit(event: FormEvent) {
    event.preventDefault();
    if (id.trim()) navigate(`/objects/${encodeURIComponent(id.trim())}`);
  }
  return (
    <Page>
      <PageHeader title="Object lookup" />
      <Panel title="Inspect an object">
        <div className={surfaceStyles.panelBody}>
          <form className={surfaceStyles.toolbar} onSubmit={submit}>
            <label className="srOnly" htmlFor="object-id">
              Object ID
            </label>
            <input
              id="object-id"
              className={`${surfaceStyles.input} ${surfaceStyles.mono} ${surfaceStyles.lookupInput}`}
              placeholder="Object ID"
              value={id}
              onChange={(event) => setId(event.target.value)}
            />
            <Button type="submit" variant="primary" disabled={!id.trim()}>
              <PackageSearch size={14} aria-hidden="true" /> Inspect
            </Button>
          </form>
        </div>
      </Panel>
    </Page>
  );
}

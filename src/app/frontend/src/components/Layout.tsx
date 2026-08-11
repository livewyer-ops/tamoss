import {
  Boxes,
  FolderInput,
  Gauge,
  Library,
  Menu,
  PackageSearch,
  Radio,
  ServerCog,
  SlidersHorizontal,
  Trash2,
  Webhook,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet } from "react-router";
import TamossMark from "@/components/TamossMark";
import styles from "./Layout.module.css";

const navigation = [
  {
    label: "Workspace",
    items: [{ to: "/", label: "Overview", icon: Gauge, end: true }],
  },
  {
    label: "Library",
    items: [
      { to: "/sources", label: "Sources", icon: Library },
      { to: "/flows", label: "Flows", icon: Radio },
      { to: "/profiles", label: "Profiles", icon: SlidersHorizontal },
      { to: "/objects", label: "Object lookup", icon: PackageSearch },
    ],
  },
  {
    label: "Operations",
    items: [
      { to: "/ingest", label: "Tamsin jobs", icon: FolderInput },
      { to: "/deletions", label: "Deletion requests", icon: Trash2 },
      { to: "/webhooks", label: "Webhooks", icon: Webhook },
    ],
  },
  {
    label: "System",
    items: [
      { to: "/system", label: "Runtime", icon: Boxes },
      { to: "/service", label: "TAMS service", icon: ServerCog },
    ],
  },
] as const;

export default function Layout() {
  const [open, setOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);

  useEffect(() => {
    if (open) {
      closeButton.current?.focus();
    } else if (wasOpen.current) {
      menuButton.current?.focus();
    }
    wasOpen.current = open;
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [open]);

  return (
    <div className={styles.shell}>
      <header className={styles.mobileHeader} inert={open}>
        <button
          ref={menuButton}
          type="button"
          className={styles.menuButton}
          onClick={() => setOpen(true)}
          aria-label="Open navigation"
          aria-controls="app-navigation"
          aria-expanded={open}
        >
          <Menu size={20} aria-hidden="true" />
        </button>
        <TamossMark className={styles.mobileMark} />
        <strong>TAMOSS</strong>
      </header>

      {open ? (
        <button
          type="button"
          className={styles.scrim}
          onClick={() => setOpen(false)}
          aria-label="Close navigation"
        />
      ) : null}

      <aside
        id="app-navigation"
        className={`${styles.sidebar} ${open ? styles.open : ""}`}
        aria-label={open ? "Navigation" : undefined}
      >
        <div className={styles.brand}>
          <TamossMark className={styles.mark} />
          <div>
            <strong>TAMOSS</strong>
            <span>Operations console</span>
          </div>
          <button
            ref={closeButton}
            type="button"
            className={styles.closeButton}
            onClick={() => setOpen(false)}
            aria-label="Close navigation"
          >
            <X size={18} aria-hidden="true" />
          </button>
        </div>

        <nav className={styles.navigation} aria-label="Main navigation">
          {navigation.map((section) => (
            <div className={styles.navSection} key={section.label}>
              <p>{section.label}</p>
              {section.items.map(({ to, label, icon: Icon, ...item }) => (
                <NavLink
                  key={to}
                  to={to}
                  end={"end" in item ? item.end : false}
                  onClick={() => setOpen(false)}
                  className={({ isActive }) =>
                    `${styles.navLink} ${isActive ? styles.active : ""}`
                  }
                >
                  <Icon size={17} strokeWidth={1.8} aria-hidden="true" />
                  <span>{label}</span>
                </NavLink>
              ))}
            </div>
          ))}
        </nav>
      </aside>

      <div className={styles.workspace} inert={open}>
        <main className={styles.main}>
          <Outlet />
        </main>
      </div>
    </div>
  );
}

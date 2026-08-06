import { useState } from "react";
import { NavLink, Outlet } from "react-router";
import TamossMark from "@/components/TamossMark";

const navSections = [
  {
    title: "Control",
    items: [
      {
        to: "/",
        label: "Dashboard",
        icon: "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1",
      },
      {
        to: "/service",
        label: "Service",
        icon: "M3.75 3v11.25A2.25 2.25 0 006 16.5h12M3.75 3h10.5A2.25 2.25 0 0116.5 5.25v10.5M3.75 3l4.5 4.5m0 0L12.75 3m-4.5 4.5h8.25",
      },
    ],
  },
  {
    title: "Media",
    items: [
      {
        to: "/sources",
        label: "Sources",
        icon: "M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10",
      },
      {
        to: "/flows",
        label: "Flows",
        icon: "M7 4V2m0 2a2 2 0 100 4m0-4a2 2 0 110 4m0 0v14m0-4a2 2 0 100-4m0 4a2 2 0 110-4m10-2a2 2 0 100-4m0 4a2 2 0 110-4m0 0V6",
      },
      {
        to: "/playback",
        label: "Playback",
        badge: "Preview",
        icon: "M5.25 5.653c0-1.427 1.529-2.33 2.779-1.643l10.424 5.735c1.295.712 1.295 2.573 0 3.285L8.029 18.765c-1.25.687-2.779-.216-2.779-1.643V5.653z",
      },
      {
        to: "/objects",
        label: "Objects",
        icon: "M3.75 3v11.25A2.25 2.25 0 006 16.5h12M3.75 3h10.5A2.25 2.25 0 0116.5 5.25v10.5m-12.75-12.75h10.5A2.25 2.25 0 0116.5 5.25m0 10.5H18a2.25 2.25 0 012.25 2.25V21m-3.75-5.25h-9A2.25 2.25 0 005.25 18v.75A2.25 2.25 0 007.5 21h9A2.25 2.25 0 0018.75 18v-.75A2.25 2.25 0 0016.5 15.75z",
      },
    ],
  },
  {
    title: "Operations",
    items: [
      {
        to: "/ingest",
        label: "Ingest",
        badge: "Preview",
        icon: "M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-13.5-9L12 3m0 0l4.5 4.5M12 3v13.5",
      },
      {
        to: "/deletions",
        label: "Deletions",
        icon: "M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16",
      },
      {
        to: "/webhooks",
        label: "Webhooks",
        icon: "M7.5 8.25h9m-9 3h9m-9 3h5.25M6.75 3h10.5A2.25 2.25 0 0119.5 5.25v13.5A2.25 2.25 0 0117.25 21H6.75A2.25 2.25 0 014.5 18.75V5.25A2.25 2.25 0 016.75 3z",
      },
    ],
  },
];

export default function Layout() {
  const [mobileOpen, setMobileOpen] = useState(false);

  return (
    <div className="tamoss-shell tamoss-grid-bg flex h-screen overflow-hidden">
      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/50 md:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden="true"
        />
      )}

      {/* Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-40 flex w-72 flex-col border-r border-white/10 bg-lw-ink-900 text-white transition-transform md:static md:translate-x-0 ${
          mobileOpen ? "translate-x-0" : "-translate-x-full"
        }`}
        role="navigation"
        aria-label="Main navigation"
      >
        <div className="relative overflow-hidden border-b border-white/10 px-4 pb-5 pt-4 sm:px-6 sm:pb-6 sm:pt-5">
          <div className="relative flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="flex h-11 w-11 items-center justify-center rounded-[10px] bg-transparent p-0 ring-1 ring-white/20">
                <TamossMark className="h-11 w-11" />
              </div>
              <div className="flex flex-col">
                <span className="text-lg font-semibold leading-tight text-white">
                  TAMOSS
                </span>
                <span className="text-[0.65rem] leading-tight text-white/50">
                  Open Source Store
                </span>
              </div>
            </div>
            <div className="hidden rounded-full border border-white/10 bg-white/5 px-2.5 py-1 text-[0.65rem] font-medium uppercase tracking-[0.2em] text-white/70 sm:block">
              Console
            </div>
          </div>
          <button
            onClick={() => setMobileOpen(false)}
            className="absolute right-4 top-4 rounded-md p-1 text-white/60 hover:text-white md:hidden"
            aria-label="Close menu"
          >
            <svg
              className="h-5 w-5"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>
        <nav className="flex-1 space-y-7 overflow-y-auto px-3 py-5">
          {navSections.map((section) => (
            <div key={section.title}>
              <p className="px-3 pb-2 text-[0.65rem] font-semibold uppercase tracking-[0.24em] text-white/35">
                {section.title}
              </p>
              <div className="space-y-1">
                {section.items.map((item) => (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === "/"}
                    onClick={() => setMobileOpen(false)}
                    className={({ isActive }) =>
                      `group flex items-center gap-3 rounded-2xl px-3 py-2.5 text-sm font-medium transition-all ${
                        isActive
                          ? "bg-white/10 text-white ring-1 ring-inset ring-white/12"
                          : "text-white/68 hover:bg-white/6 hover:text-white"
                      }`
                    }
                  >
                    {({ isActive }) => (
                      <>
                        <svg
                          className="h-5 w-5 flex-shrink-0"
                          fill="none"
                          viewBox="0 0 24 24"
                          strokeWidth={1.5}
                          stroke="currentColor"
                          aria-hidden="true"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            d={item.icon}
                          />
                        </svg>
                        <span className="flex-1">{item.label}</span>
                        {"badge" in item && item.badge && (
                          <span className="rounded-full border border-amber-300/30 bg-amber-300/10 px-1.5 py-0.5 text-[0.58rem] font-semibold uppercase tracking-[0.16em] text-amber-100">
                            {item.badge}
                          </span>
                        )}
                        <span
                          className={`h-2 w-2 rounded-full transition-opacity ${
                            isActive
                              ? "bg-tams-400 opacity-100"
                              : "bg-white/20 opacity-0"
                          }`}
                          aria-hidden="true"
                        />
                      </>
                    )}
                  </NavLink>
                ))}
              </div>
            </div>
          ))}
        </nav>
        <div className="border-t border-white/8 px-4 py-4 text-xs text-white/45 sm:px-6">
          Time Addressable Media Open Source Store
        </div>
      </aside>

      {/* Main content */}
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Mobile header */}
        <header className="tamoss-panel flex h-16 items-center gap-3 border-b border-transparent px-4 md:hidden">
          <button
            onClick={() => setMobileOpen(true)}
            className="rounded-xl p-2 text-lw-ink-700 hover:bg-lw-ink-50 hover:text-lw-ink-900"
            aria-label="Open menu"
          >
            <svg
              className="h-6 w-6"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M3.75 6.75h16.5M3.75 12h16.5m-16.5 5.25h16.5"
              />
            </svg>
          </button>
          <div className="flex items-center gap-2">
            <div className="flex h-9 w-9 items-center justify-center rounded-[8px] bg-transparent p-0 ring-1 ring-lw-ink-100">
              <TamossMark className="h-9 w-9" />
            </div>
            <span className="font-semibold text-lw-ink-900">TAMOSS</span>
          </div>
        </header>
        <main className="flex-1 overflow-auto" role="main">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

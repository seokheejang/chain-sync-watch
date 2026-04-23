"use client";

import {
  Activity,
  CalendarClock,
  Database,
  GitCompare,
  LayoutDashboard,
  ListOrdered,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { ThemeToggle } from "@/components/shared/theme-toggle";
import { cn } from "@/lib/utils";

type NavItem = {
  href: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
};

const nav: NavItem[] = [
  { href: "/", label: "Dashboard", icon: LayoutDashboard },
  { href: "/runs", label: "Runs", icon: ListOrdered },
  { href: "/diffs", label: "Discrepancies", icon: GitCompare },
  { href: "/schedules", label: "Schedules", icon: CalendarClock },
  { href: "/sources", label: "Sources", icon: Database },
];

// AppShell is the top-level frame every page renders inside. Header
// pins a compact brand + theme toggle; the sidebar pins navigation
// and highlights the current route so operators can pivot between
// runs, diffs, and schedules without re-reading labels.
export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-full w-full">
      <aside className="hidden w-60 shrink-0 flex-col border-r bg-muted/20 md:flex">
        <div className="flex h-14 items-center gap-2 border-b px-4">
          <Activity className="h-5 w-5 text-primary" />
          <span className="font-semibold tracking-tight">chain-sync-watch</span>
        </div>
        <nav className="flex-1 space-y-1 p-2">
          {nav.map((item) => {
            const Icon = item.icon;
            const active = pathname === item.href || pathname.startsWith(`${item.href}/`);
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  active
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </Link>
            );
          })}
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between gap-2 border-b px-4 md:px-6">
          <div className="flex items-center gap-2 md:hidden">
            <Activity className="h-5 w-5 text-primary" />
            <span className="font-semibold tracking-tight">chain-sync-watch</span>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <ThemeToggle />
          </div>
        </header>
        <main className="flex-1 overflow-auto px-4 py-6 md:px-6">{children}</main>
      </div>
    </div>
  );
}

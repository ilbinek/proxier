import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useTheme } from "next-themes";
import {
  ArrowLeftRight,
  Ban,
  Cable,
  LayoutDashboard,
  LayoutTemplate,
  LogOut,
  Menu,
  Monitor,
  Moon,
  ScrollText,
  Settings,
  Sun,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { fetchLiveHistory, qk, useAuthStatus } from "@/hooks/queries";
import { api } from "@/lib/api";
import { fmtRate } from "@/lib/format";
import { connectLive, onLiveEvent, useLive } from "@/lib/live";
import { cn } from "@/lib/utils";

const nav = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard, end: true },
  { to: "/rules", label: "Rules", icon: ArrowLeftRight },
  { to: "/connections", label: "Connections", icon: Cable },
  { to: "/presets", label: "Presets", icon: LayoutTemplate },
  { to: "/bans", label: "Bans", icon: Ban },
  { to: "/events", label: "Events", icon: ScrollText },
  { to: "/settings", label: "Settings", icon: Settings },
];

function Logo() {
  return (
    <div className="flex items-center gap-2 px-2">
      <div className="flex size-8 items-center justify-center rounded-lg bg-foreground text-background">
        <ArrowLeftRight className="size-4" />
      </div>
      <span className="text-lg font-semibold tracking-tight">proxier</span>
    </div>
  );
}

function NavItems({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex flex-col gap-0.5">
      {nav.map(({ to, label, icon: Icon, end }) => (
        <NavLink
          key={to}
          to={to}
          end={end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
              isActive && "bg-accent text-foreground",
            )
          }
        >
          <Icon className="size-4" />
          {label}
        </NavLink>
      ))}
    </nav>
  );
}

function LiveIndicator() {
  const status = useLive((s) => s.status);
  const latest = useLive((s) => s.samples.at(-1));
  const label = status === "open" ? "Live" : status === "connecting" ? "Connecting…" : "Offline";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center gap-2 rounded-full border px-3 py-1 text-xs">
          <span
            className={cn(
              "size-2 rounded-full",
              status === "open" ? "bg-success" : status === "connecting" ? "bg-warning animate-pulse" : "bg-destructive",
            )}
          />
          <span className="font-medium">{label}</span>
          {status === "open" && latest && (
            <span className="hidden tabular text-muted-foreground sm:inline">
              ↓ {fmtRate(latest.out_bps)} · ↑ {fmtRate(latest.in_bps)}
            </span>
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent>
        {status === "open" ? "Receiving live updates" : "Live updates unavailable, reconnecting"}
      </TooltipContent>
    </Tooltip>
  );
}

function ThemeToggle() {
  const { setTheme, resolvedTheme } = useTheme();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Theme">
          {resolvedTheme === "dark" ? <Moon /> : <Sun />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => setTheme("light")}><Sun /> Light</DropdownMenuItem>
        <DropdownMenuItem onClick={() => setTheme("dark")}><Moon /> Dark</DropdownMenuItem>
        <DropdownMenuItem onClick={() => setTheme("system")}><Monitor /> System</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Wires the WebSocket to the live store and throttles query invalidation. */
function useLiveWiring() {
  const qc = useQueryClient();
  useEffect(() => {
    const stop = connectLive();
    fetchLiveHistory()
      .then((h) => useLive.getState().seed(h.history ?? []))
      .catch(() => {});

    const pending = new Set<string>();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const flush = () => {
      timer = undefined;
      for (const k of pending) qc.invalidateQueries({ queryKey: [k] });
      pending.clear();
    };
    const unsub = onLiveEvent((e) => {
      pending.add("events");
      if (e.type.startsWith("conn_")) pending.add("connections");
      if (e.type.startsWith("rule_") || e.type === "preset_applied" || e.type === "config_imported") pending.add("rules");
      if (e.type.startsWith("ban_") || e.type === "config_imported") pending.add("bans");
      if (e.type.startsWith("preset_")) pending.add("presets");
      if (e.type === "settings_changed") pending.add("settings");
      timer ??= setTimeout(flush, 1000);
    });
    return () => {
      stop();
      unsub();
      clearTimeout(timer);
    };
  }, [qc]);
}

export function AppShell() {
  useLiveWiring();
  const qc = useQueryClient();
  const auth = useAuthStatus();
  const [mobileOpen, setMobileOpen] = useState(false);
  const location = useLocation();

  useEffect(() => setMobileOpen(false), [location.pathname]);

  const logout = async () => {
    await api("/auth/logout", { method: "POST" }).catch(() => {});
    qc.clear();
    await qc.invalidateQueries({ queryKey: qk.auth });
  };

  return (
    <div className="min-h-svh bg-background">
      <aside className="fixed inset-y-0 left-0 hidden w-60 flex-col border-r bg-sidebar p-4 lg:flex">
        <Logo />
        <div className="mt-8 flex-1">
          <NavItems />
        </div>
        <div className="space-y-1 text-xs text-muted-foreground">
          <div className="px-3">proxier {auth.data?.version}</div>
        </div>
      </aside>

      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-64 p-4">
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <Logo />
          <div className="mt-8">
            <NavItems onNavigate={() => setMobileOpen(false)} />
          </div>
        </SheetContent>
      </Sheet>

      <div className="lg:pl-60">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60 sm:px-6">
          <Button variant="ghost" size="icon" className="lg:hidden" onClick={() => setMobileOpen(true)} aria-label="Menu">
            <Menu />
          </Button>
          <div className="lg:hidden">
            <Logo />
          </div>
          <div className="ml-auto flex items-center gap-1">
            <LiveIndicator />
            <ThemeToggle />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" onClick={logout} aria-label="Log out">
                  <LogOut />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Log out</TooltipContent>
            </Tooltip>
          </div>
        </header>
        <main className="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:py-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

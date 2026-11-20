import { Loader2 } from "lucide-react";
import { Navigate, Route, Routes } from "react-router";
import { AppShell } from "@/components/AppShell";
import { useAuthStatus } from "@/hooks/queries";
import { LoginPage, SetupPage } from "@/pages/Auth";
import { BansPage } from "@/pages/Bans";
import { ConnectionsPage } from "@/pages/Connections";
import { DashboardPage } from "@/pages/Dashboard";
import { EventsPage } from "@/pages/Events";
import { PresetsPage } from "@/pages/Presets";
import { RuleDetailPage } from "@/pages/RuleDetail";
import { RulesPage } from "@/pages/Rules";
import { SettingsPage } from "@/pages/Settings";

export default function App() {
  const auth = useAuthStatus();

  if (auth.isLoading) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Loader2 className="animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (auth.isError) {
    return (
      <div className="flex min-h-svh flex-col items-center justify-center gap-2 p-4 text-center">
        <p className="font-medium">Can't reach the proxier server.</p>
        <button className="text-sm underline" onClick={() => auth.refetch()}>Retry</button>
      </div>
    );
  }
  if (auth.data?.needs_setup) return <SetupPage />;
  if (!auth.data?.authenticated) return <LoginPage />;

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<DashboardPage />} />
        <Route path="rules" element={<RulesPage />} />
        <Route path="rules/:id" element={<RuleDetailPage />} />
        <Route path="connections" element={<ConnectionsPage />} />
        <Route path="presets" element={<PresetsPage />} />
        <Route path="bans" element={<BansPage />} />
        <Route path="events" element={<EventsPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

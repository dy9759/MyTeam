import { useEffect, useState } from "react";
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
} from "react-router-dom";
import { DesktopShell } from "@/components/desktop-shell";
import { bootstrapDesktopApp, useDesktopAuthStore } from "@/lib/desktop-client";
import { LoginRoute } from "@/routes/login-route";
import { SessionRoute } from "@/routes/session-route";
import { ProjectsRoute } from "@/routes/projects-route";
import { FilesRoute } from "@/routes/files-route";
import { AccountRoute } from "@/routes/account-route";
import { SettingsRoute } from "@/routes/settings-route";

export function App() {
  const [isBootstrapping, setIsBootstrapping] = useState(true);
  const user = useDesktopAuthStore((state) => state.user);

  useEffect(() => {
    void bootstrapDesktopApp().finally(() => setIsBootstrapping(false));
  }, []);

  if (isBootstrapping) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <div className="rounded-3xl border border-border/70 bg-card/85 px-6 py-5 text-sm text-muted-foreground">
          Bootstrapping MyTeam desktop…
        </div>
      </div>
    );
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route
          path="/login"
          element={user ? <Navigate to="/session" replace /> : <LoginRoute />}
        />
        <Route
          path="/"
          element={user ? <DesktopShell /> : <Navigate to="/login" replace />}
        >
          <Route index element={<Navigate to="/session" replace />} />
          <Route path="session" element={<SessionRoute />} />
          <Route path="projects" element={<ProjectsRoute />} />
          <Route path="files" element={<FilesRoute />} />
          <Route path="account" element={<AccountRoute />} />
          <Route path="settings" element={<SettingsRoute />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

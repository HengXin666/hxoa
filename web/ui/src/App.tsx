import * as React from "react";
import { Loader2 } from "lucide-react";
import { api, type AuthInfo } from "@/lib/api";
import Login from "@/pages/Login";
import Dashboard from "@/pages/Dashboard";

export default function App() {
  const [auth, setAuth] = React.useState<AuthInfo | null>(null);
  const [err, setErr] = React.useState("");
  const refresh = React.useCallback(async () => {
    try {
      setAuth(await api<AuthInfo>("GET", "api/auth"));
      setErr("");
    } catch (e: any) {
      setErr(e.message);
    }
  }, []);
  React.useEffect(() => {
    refresh();
  }, [refresh]);

  if (!auth)
    return (
      <div className="flex min-h-svh items-center justify-center gap-2 text-muted-foreground">
        {err ? <span className="text-destructive">后端不可用: {err}</span> : <><Loader2 className="size-4 animate-spin" />加载中</>}
      </div>
    );
  if (!auth.logged_in) return <Login onDone={refresh} />;
  return <Dashboard auth={auth} onAuthChange={refresh} />;
}

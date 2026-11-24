import * as React from "react";
import { toast } from "sonner";
import { CircleUser, FolderGit2, KeyRound, LogOut, ShieldCheck } from "lucide-react";
import { api, ApiError, type AuthInfo, type Op, type State } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import TunnelCard from "@/components/TunnelCard";
import AddWorkspace from "@/components/AddWorkspace";
import WorkspaceCard from "@/components/WorkspaceCard";
import OpsCard from "@/components/OpsCard";
import AccountDialog from "@/components/AccountDialog";
import PromptDialog from "@/components/PromptDialog";

export default function Dashboard({ auth, onAuthChange }: { auth: AuthInfo; onAuthChange: () => void }) {
  const [state, setState] = React.useState<State | null>(null);
  const [ops, setOps] = React.useState<Op[]>([]);
  const [prompt, setPrompt] = React.useState<{ label: string; text: string } | null>(null);
  const [account, setAccount] = React.useState(false);
  const [down, setDown] = React.useState("");
  const pause = React.useRef(false);

  const load = React.useCallback(async () => {
    if (pause.current) return;
    try {
      const [s, o] = await Promise.all([api<State>("GET", "api/state"), api<{ ops: Op[] }>("GET", "api/ops")]);
      setState(s);
      setOps(o.ops);
      setDown("");
    } catch (e) {
      if (e instanceof ApiError && e.code === "login") onAuthChange();
      else setDown((e as Error).message);
    }
  }, [onAuthChange]);

  React.useEffect(() => {
    load();
    const t = setInterval(load, 3000);
    return () => clearInterval(t);
  }, [load]);

  const run = React.useCallback(
    async (fn: () => Promise<unknown>, ok?: string) => {
      pause.current = true;
      try {
        await fn();
        if (ok) toast.success(ok);
      } catch (e) {
        toast.error((e as Error).message);
      } finally {
        pause.current = false;
        load();
      }
    },
    [load],
  );

  async function logout() {
    await api("POST", "api/logout").catch(() => {});
    onAuthChange();
  }

  const live = !!state?.public_url && (state.tunnel.state === "running" || !!state.tunnel.fixed || state.tunnel.mode === "external");

  return (
    <div className="min-h-svh bg-muted/40">
      <header className="bg-background/80 sticky top-0 z-40 border-b backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-3 px-4">
          <div className="bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-md">
            <FolderGit2 className="size-4" />
          </div>
          <span className="font-semibold">hxoa 工作区</span>
          {state && <span className="text-muted-foreground hidden text-xs sm:inline">{state.server}</span>}
          <div className="ml-auto flex items-center gap-2">
            {down ? (
              <Badge variant="destructive">后端不可用</Badge>
            ) : state?.public_url ? (
              <Badge variant={live ? "success" : "secondary"} className="hidden max-w-72 truncate font-mono md:inline-flex">
                {state.public_url}
              </Badge>
            ) : (
              <Badge variant="secondary">公网地址未就绪</Badge>
            )}
            {auth.mode === "none" ? (
              <Badge variant="outline" className="gap-1">
                <ShieldCheck />上游认证
              </Badge>
            ) : (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="sm">
                    <CircleUser />
                    {auth.username || "未设置账号"}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuLabel className="text-muted-foreground text-xs font-normal">
                    {auth.has_account ? `已登录: ${auth.username}` : "还没有设置账号"}
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={() => setAccount(true)}>
                    <KeyRound />
                    {auth.has_account ? "修改账号密码" : "设置账号密码"}
                  </DropdownMenuItem>
                  {auth.has_account && (
                    <DropdownMenuItem variant="destructive" onSelect={logout}>
                      <LogOut />
                      退出登录
                    </DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </div>
      </header>

      <main className="mx-auto grid max-w-6xl gap-5 px-4 py-6">
        {auth.seeded && (
          <div className="bg-background flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/40 px-4 py-3 text-sm">
            <KeyRound className="size-4 text-amber-600" />
            <span>你正在用安装时生成的初始密码 (用户 admin)。建议改成自己的账号密码。</span>
            <Button size="sm" variant="outline" className="ml-auto" onClick={() => setAccount(true)}>
              去修改
            </Button>
          </div>
        )}
        {state && (
          <>
            <TunnelCard tunnel={state.tunnel} publicUrl={state.public_url} run={run} />
            <AddWorkspace run={run} />
            {state.workspaces.length === 0 ? (
              <p className="text-muted-foreground px-1 text-sm">还没有工作区。先在上面选一个目录。</p>
            ) : (
              state.workspaces.map((w) => (
                <WorkspaceCard key={w.id} ws={w} run={run} onPrompt={(label, text) => setPrompt({ label, text })} />
              ))
            )}
            <OpsCard ops={ops} />
          </>
        )}
      </main>

      <PromptDialog value={prompt} onClose={() => setPrompt(null)} />
      <AccountDialog open={account} onOpenChange={setAccount} auth={auth} onSaved={onAuthChange} />
    </div>
  );
}

export type Run = (fn: () => Promise<unknown>, ok?: string) => Promise<void>;

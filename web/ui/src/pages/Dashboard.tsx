import * as React from "react";
import { toast } from "sonner";
import { Activity, CircleUser, FolderGit2, Globe, KeyRound, LogOut, Plus, ShieldCheck, Smartphone } from "lucide-react";
import { api, ApiError, type AuthInfo, type Op, type State } from "@/lib/api";
import { cn } from "@/lib/utils";
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
import NetworkView, { tunnelHealth } from "@/components/NetworkView";
import AddWorkspaceDialog from "@/components/AddWorkspace";
import WorkspaceView from "@/components/WorkspaceView";
import WorkspaceTree from "@/components/WorkspaceTree";
import OpsTable from "@/components/OpsCard";
import AccountDialog from "@/components/AccountDialog";
import PromptDialog from "@/components/PromptDialog";
import RemoteAdminView from "@/components/RemoteAdminView";

export type Run = (fn: () => Promise<unknown>, ok?: string) => Promise<void>;

type View = { kind: "ws"; id: string } | { kind: "net" } | { kind: "ops" } | { kind: "remote" };

function readHash(): View | null {
  const h = decodeURIComponent(location.hash.slice(1));
  if (h === "net") return { kind: "net" };
  if (h === "ops") return { kind: "ops" };
  if (h === "remote") return { kind: "remote" };
  if (h.startsWith("ws/")) return { kind: "ws", id: h.slice(3) };
  return null;
}

export default function Dashboard({ auth, onAuthChange }: { auth: AuthInfo; onAuthChange: () => void }) {
  const [state, setState] = React.useState<State | null>(null);
  const [ops, setOps] = React.useState<Op[]>([]);
  const [prompt, setPrompt] = React.useState<{ label: string; text: string } | null>(null);
  const [account, setAccount] = React.useState(false);
  const [adding, setAdding] = React.useState(false);
  const [down, setDown] = React.useState("");
  const [view, setViewRaw] = React.useState<View | null>(readHash);
  const pause = React.useRef(false);

  const setView = (v: View) => {
    setViewRaw(v);
    history.replaceState(null, "", "#" + (v.kind === "ws" ? "ws/" + v.id : v.kind));
  };

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

  const run = React.useCallback<Run>(
    async (fn, ok) => {
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

  // Fall back to a sensible view when nothing (or a removed workspace) is selected.
  const workspaces = state?.workspaces ?? [];
  let current: View | null = view;
  if (state && (!current || (current.kind === "ws" && !workspaces.some((w) => w.id === (current as { id: string }).id)))) {
    current = workspaces.length ? { kind: "ws", id: workspaces[0].id } : { kind: "net" };
  }
  const ws = current?.kind === "ws" ? workspaces.find((w) => w.id === current.id) : undefined;
  const health = state ? tunnelHealth(state.tunnel, state.public_url) : null;

  return (
    <div className="flex h-svh flex-col">
      <header className="bg-background flex h-14 shrink-0 items-center gap-3 border-b px-4">
        <div className="bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-md">
          <FolderGit2 className="size-4" />
        </div>
        <span className="font-semibold">hxoa</span>
        {state && <span className="text-muted-foreground hidden text-xs sm:inline">{state.server}</span>}
        <div className="ml-auto flex items-center gap-2">
          {down && <Badge variant="destructive">后端不可用</Badge>}
          {auth.mode === "none" ? (
            <Badge variant="outline" className="gap-1">
              <ShieldCheck />
              上游认证
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
      </header>

      <div className="flex min-h-0 flex-1">
        <aside className="bg-muted/30 flex w-72 shrink-0 flex-col border-r">
          <div className="flex items-center justify-between px-4 pt-4 pb-2">
            <span className="text-muted-foreground text-xs font-medium">工作区</span>
            <Button size="icon-sm" variant="ghost" onClick={() => setAdding(true)} title="添加工作区">
              <Plus />
            </Button>
          </div>
          <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-2">
            <WorkspaceTree
              workspaces={workspaces}
              selected={current?.kind === "ws" ? current.id : undefined}
              onSelect={(id) => setView({ kind: "ws", id })}
            />
            {state && workspaces.length === 0 && (
              <button
                onClick={() => setAdding(true)}
                className="text-muted-foreground hover:bg-accent flex w-full cursor-pointer items-center gap-2 rounded-md border border-dashed px-3 py-3 text-sm"
              >
                <Plus className="size-4" />
                添加第一个工作区
              </button>
            )}
          </nav>
          <div className="space-y-0.5 border-t p-2">
            <SideItem active={current?.kind === "net"} onClick={() => setView({ kind: "net" })} icon={<Globe />}>
              公网访问
              {health && (
                <span className={cn("ml-auto size-2 rounded-full", health.tone === "ok" ? "bg-success" : health.tone === "bad" ? "bg-destructive" : "bg-amber-500")} />
              )}
            </SideItem>
            <SideItem active={current?.kind === "remote"} onClick={() => setView({ kind: "remote" })} icon={<Smartphone />}>
              远程管理
            </SideItem>
            <SideItem active={current?.kind === "ops"} onClick={() => setView({ kind: "ops" })} icon={<Activity />}>
              操作记录
            </SideItem>
          </div>
        </aside>

        <main className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto max-w-4xl px-8 py-6">
            {auth.seeded && (
              <div className="bg-background mb-5 flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/40 px-4 py-3 text-sm">
                <KeyRound className="size-4 text-amber-600" />
                <span>你正在用安装时生成的初始密码 (用户 admin)。建议改成自己的账号密码。</span>
                <Button size="sm" variant="outline" className="ml-auto" onClick={() => setAccount(true)}>
                  去修改
                </Button>
              </div>
            )}
            {state && current?.kind === "net" && <NetworkView tunnel={state.tunnel} publicUrl={state.public_url} run={run} />}
            {state && current?.kind === "remote" && <RemoteAdminView run={run} publicUrl={state.public_url} />}
            {state && current?.kind === "ops" && (
              <section className="grid gap-4">
                <h1 className="text-xl font-semibold">操作记录</h1>
                <OpsTable ops={ops} />
              </section>
            )}
            {state && ws && (
              <WorkspaceView
                key={ws.id}
                ws={ws}
                ops={ops.filter((o) => o.ws === ws.name)}
                publicReady={health?.tone === "ok"}
                run={run}
                onPrompt={(label, text) => setPrompt({ label, text })}
                onOpenNet={() => setView({ kind: "net" })}
              />
            )}
          </div>
        </main>
      </div>

      <AddWorkspaceDialog
        open={adding}
        onOpenChange={setAdding}
        run={run}
        existing={workspaces.map((w) => w.path)}
        onAdded={(id) => {
          setAdding(false);
          if (id) setView({ kind: "ws", id });
        }}
      />
      <PromptDialog value={prompt} onClose={() => setPrompt(null)} />
      <AccountDialog open={account} onOpenChange={setAccount} auth={auth} onSaved={onAuthChange} />
    </div>
  );
}

function SideItem({ active, onClick, icon, children }: { active: boolean; onClick: () => void; icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex w-full cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors [&_svg]:size-4",
        active ? "bg-background font-medium shadow-xs ring-1 ring-border" : "text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      {icon}
      {children}
    </button>
  );
}

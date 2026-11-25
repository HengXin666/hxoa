import * as React from "react";
import { ChevronRight, CornerLeftUp, Folder, GitBranch, Home, Loader2, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { Mono } from "@/components/ui/mono";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

interface Browse {
  path: string;
  parent: string;
  dirs: string[];
  git: boolean;
}

export default function AddWorkspaceDialog({
  open,
  onOpenChange,
  run,
  onAdded,
  existing,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  run: Run;
  onAdded: (id?: string) => void;
  existing: string[];
}) {
  const [picked, setPicked] = React.useState<string[]>([]);
  const had = React.useMemo(() => new Set(existing), [existing]);
  const togglePick = (p: string, on: boolean) => setPicked((l) => (on ? (l.includes(p) ? l : [...l, p]) : l.filter((x) => x !== p)));
  const [cur, setCur] = React.useState<Browse | null>(null);
  const [typed, setTyped] = React.useState("");
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [name, setName] = React.useState("");
  const [workers, setWorkers] = React.useState(false);
  const [exec, setExec] = React.useState(true);

  const browse = React.useCallback(async (p: string) => {
    setLoading(true);
    try {
      const r = await api<Browse>("GET", "api/browse?path=" + encodeURIComponent(p));
      setCur(r);
      setTyped(r.path);
      setErr("");
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    if (open && !cur) browse("");
    if (!open) {
      setName("");
      setErr("");
      setPicked([]);
    }
  }, [open, cur, browse]);

  const crumbs = React.useMemo(() => {
    if (!cur) return [];
    const parts = cur.path.split("/").filter(Boolean);
    return parts.map((p, i) => ({ name: p, path: "/" + parts.slice(0, i + 1).join("/") }));
  }, [cur]);

  // Nothing ticked: add the folder being viewed. Otherwise add every ticked one.
  const targets = picked.length ? picked : cur ? [cur.path] : [];
  const single = targets.length === 1;

  async function add() {
    if (!targets.length) return;
    let first: string | undefined;
    const failed: string[] = [];
    await run(async () => {
      for (const p of targets) {
        try {
          const r = await api<{ workspace: { id: string } }>("POST", "api/workspaces", { path: p, name: single ? name : "", workers, exec });
          first ??= r.workspace?.id;
        } catch (e) {
          failed.push(`${p}: ${(e as Error).message}`);
        }
      }
      if (failed.length) throw new Error(failed.join("\n"));
    }, single ? "已添加工作区" : `已添加 ${targets.length} 个工作区`);
    if (first) {
      if (failed.length) toast.warning(`${targets.length - failed.length} 个已添加, ${failed.length} 个失败`);
      onAdded(first);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-5 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>添加工作区</DialogTitle>
          <DialogDescription>进入一个目录直接添加, 或勾选多个子目录一次加上。AI 只能读写工作区里面的文件。</DialogDescription>
        </DialogHeader>

        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            browse(typed);
          }}
        >
          <Input value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="输入路径后回车, 如 ~/code/project" className="font-mono text-[0.8125rem]" />
          <Button type="button" variant="outline" size="icon" title="主目录" onClick={() => browse("")}>
            <Home />
          </Button>
        </form>

        <div className="overflow-hidden rounded-lg border">
          <div className="bg-muted/50 flex min-h-10 flex-wrap items-center gap-0.5 border-b px-2 py-1.5">
            <CrumbButton onClick={() => browse("/")}>/</CrumbButton>
            {crumbs.map((c, i) => (
              <React.Fragment key={c.path}>
                {i > 0 && <ChevronRight className="text-muted-foreground size-3.5" />}
                <CrumbButton onClick={() => browse(c.path)} current={i === crumbs.length - 1}>
                  {c.name}
                </CrumbButton>
              </React.Fragment>
            ))}
            {cur?.git && (
              <span className="text-success ml-auto flex items-center gap-1 pr-1 text-xs">
                <GitBranch className="size-3.5" />
                git 仓库
              </span>
            )}
          </div>
          <div className="h-72 overflow-y-auto py-1">
            {loading && !cur ? (
              <div className="text-muted-foreground flex h-full items-center justify-center gap-2 text-sm">
                <Loader2 className="size-4 animate-spin" />
                读取中
              </div>
            ) : (
              <>
                {cur && cur.path !== "/" && (
                  <Row onClick={() => browse(cur.parent)} icon={<CornerLeftUp />}>
                    上一级
                  </Row>
                )}
                {cur?.dirs.map((d) => {
                  const p = cur.path.replace(/\/$/, "") + "/" + d;
                  const done = had.has(p);
                  return (
                    <div key={d} className="hover:bg-accent flex items-center gap-3 pl-4">
                      <Checkbox checked={done || picked.includes(p)} disabled={done} onCheckedChange={(v) => togglePick(p, v === true)} aria-label={"选择 " + d} />
                      <button
                        type="button"
                        onClick={() => browse(p)}
                        className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 py-1.5 pr-4 text-left text-sm"
                      >
                        <Folder className="text-muted-foreground size-4 shrink-0" />
                        <Mono className="truncate">{d}</Mono>
                        {done && <span className="text-muted-foreground ml-auto text-xs">已添加</span>}
                      </button>
                    </div>
                  );
                })}
                {cur && cur.dirs.length === 0 && <p className="text-muted-foreground px-4 py-2 text-sm">没有子目录</p>}
              </>
            )}
          </div>
        </div>
        {err && <p className="text-destructive text-sm">{err}</p>}

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <Label htmlFor="ws-name">名字</Label>
            <Input
              id="ws-name"
              value={single ? name : ""}
              disabled={!single}
              onChange={(e) => setName(e.target.value)}
              placeholder={single ? targets[0].split("/").filter(Boolean).pop() || "/" : "多选时用各自的目录名"}
            />
          </div>
          <div className="grid content-end gap-3 pb-1">
            <Label className="cursor-pointer justify-between">
              允许执行命令
              <Switch checked={exec} onCheckedChange={setExec} />
            </Label>
            <Label className={cn("justify-between", cur?.git ? "cursor-pointer" : "opacity-50")}>
              并行编辑 (workers)
              <Switch checked={workers && !!cur?.git} disabled={!cur?.git} onCheckedChange={setWorkers} />
            </Label>
          </div>
        </div>

        {picked.length > 0 && (
          <div className="flex max-h-24 flex-wrap gap-1.5 overflow-y-auto">
            {picked.map((p) => (
              <span key={p} className="bg-secondary inline-flex max-w-full items-center gap-1 rounded-md py-0.5 pr-1 pl-2">
                <Mono className="truncate text-xs" title={p}>
                  {p}
                </Mono>
                <button type="button" onClick={() => togglePick(p, false)} className="text-muted-foreground hover:text-foreground cursor-pointer" aria-label="取消">
                  <X className="size-3.5" />
                </button>
              </span>
            ))}
          </div>
        )}

        <DialogFooter className="items-center sm:justify-between">
          <span className="text-muted-foreground min-w-0 truncate text-sm">
            {picked.length ? (
              <>已勾选 {picked.length} 个目录</>
            ) : (
              <>
                将添加 <Mono className="text-foreground">{cur?.path ?? "…"}</Mono>
              </>
            )}
          </span>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button onClick={add} disabled={!targets.length || (!picked.length && !!cur && had.has(cur.path))}>
              {picked.length > 1 ? `添加 ${picked.length} 个工作区` : picked.length === 1 ? "添加勾选的目录" : "添加当前目录"}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CrumbButton({ children, onClick, current }: { children: React.ReactNode; onClick: () => void; current?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "hover:bg-accent cursor-pointer rounded px-1.5 py-0.5 font-mono text-[0.8125rem]",
        current ? "text-foreground font-medium" : "text-muted-foreground",
      )}
    >
      {children}
    </button>
  );
}

function Row({ children, onClick, icon }: { children: React.ReactNode; onClick: () => void; icon: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-accent flex w-full cursor-pointer items-center gap-2.5 px-4 py-1.5 text-left text-sm [&>svg]:text-muted-foreground [&>svg]:size-4"
    >
      {icon}
      {children}
    </button>
  );
}

import * as React from "react";
import { AlertTriangle, ClipboardList, Pencil } from "lucide-react";
import { api, type TokenView, type WorkspaceView as WS } from "@/lib/api";
import { ago, cn } from "@/lib/utils";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Mono } from "@/components/ui/mono";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

// Who is on which issue: a connection's own registration (hxoa task, or the
// user assigning it here) and the issues named in its unmerged commits.
export interface Holder {
  label: string;
  online: boolean;
  claimed: boolean;
  committed: boolean;
}

export function issueMap(ws: WS): Map<number, Holder[]> {
  const m = new Map<number, Holder[]>();
  for (const t of ws.tokens) {
    const claimed = new Set(t.task?.issues ?? []);
    const committed = new Set(t.worker?.refs ?? []);
    for (const n of new Set([...claimed, ...committed])) {
      const list = m.get(n) ?? [];
      list.push({ label: t.label, online: t.online, claimed: claimed.has(n), committed: committed.has(n) });
      m.set(n, list);
    }
  }
  return new Map([...m.entries()].sort((a, b) => a[0] - b[0]));
}

interface IssueInfo {
  title: string;
  state: string;
  at: number;
}

const TITLE_TTL = 6 * 3600 * 1000;
const CACHE_KEY = "hxoa.issues";

function readCache(): Record<string, IssueInfo> {
  try {
    return JSON.parse(localStorage.getItem(CACHE_KEY) || "{}");
  } catch {
    return {};
  }
}

// Titles: hxoad reads them with the host's own gh login; for whatever it
// could not give, the browser asks GitHub's public API. Failures just leave
// the number alone.
export function useIssueTitles(ws: WS, nums: number[]) {
  const [info, setInfo] = React.useState<Record<string, IssueInfo>>(readCache);
  const want = nums.join(",");
  const repo = ws.repo;
  React.useEffect(() => {
    if (!repo || !want) return;
    const slug = repo.replace("https://github.com/", "");
    const key = (n: string) => `${slug}#${n}`;
    const cache = readCache();
    const fresh = (n: string) => cache[key(n)] && Date.now() - cache[key(n)].at < TITLE_TTL;
    const stale = want.split(",").filter((n) => !fresh(n));
    if (!stale.length) return;
    let live = true;
    (async () => {
      try {
        const r = await api<{ issues: Record<string, { title: string; state: string }> | null }>("GET", `api/workspaces/${ws.id}/issues`);
        for (const [n, v] of Object.entries(r.issues ?? {})) cache[key(n)] = { ...v, at: Date.now() };
      } catch {}
      await Promise.all(
        stale
          .filter((n) => !fresh(n))
          .map(async (n) => {
            try {
              const r = await fetch(`https://api.github.com/repos/${slug}/issues/${n}`, { headers: { Accept: "application/vnd.github+json" } });
              if (!r.ok) return;
              const j = await r.json();
              cache[key(n)] = { title: j.title, state: j.state, at: Date.now() };
            } catch {}
          }),
      );
      localStorage.setItem(CACHE_KEY, JSON.stringify(cache));
      if (live) setInfo({ ...cache });
    })();
    return () => {
      live = false;
    };
  }, [repo, ws.id, want]);
  return (n: number) => (repo ? info[`${repo.replace("https://github.com/", "")}#${n}`] : undefined);
}

export function IssueChip({ n, repo, faint, title }: { n: number; repo?: string; faint?: boolean; title?: string }) {
  const body = <span className={cn("rounded border px-1.5 py-0.5 font-mono text-xs", faint ? "border-dashed text-muted-foreground" : "bg-secondary")}>#{n}</span>;
  if (!repo) return body;
  return (
    <a href={`${repo}/issues/${n}`} target="_blank" rel="noreferrer" title={title} className="hover:opacity-80">
      {body}
    </a>
  );
}

// Chips on a connection row: solid = registered, dashed = only in commits.
export function TokenIssues({ t, repo, onEdit }: { t: TokenView; repo?: string; onEdit: () => void }) {
  const claimed = t.task?.issues ?? [];
  const extra = (t.worker?.refs ?? []).filter((n) => !claimed.includes(n));
  return (
    <div className="mt-1 flex flex-wrap items-center gap-1 pl-4 text-xs">
      {claimed.map((n) => (
        <IssueChip key={n} n={n} repo={repo} />
      ))}
      {extra.map((n) => (
        <IssueChip key={n} n={n} repo={repo} faint title="只在提交里出现, 没登记" />
      ))}
      {t.task?.paths?.map((p) => (
        <Badge key={p} variant="outline" title="认领的目录, 别人改到会被提醒">
          <Mono className="text-xs">{p}</Mono>
        </Badge>
      ))}
      {t.task?.note && <span className="text-muted-foreground">{t.task.note}</span>}
      {t.check && (
        <Badge variant={t.check.ok && !t.check.skipped ? "success" : "destructive"} title={`最近一次 commit 门禁, ${ago(t.check.at)}`}>
          {t.check.skipped ? "跳过了 checks" : t.check.ok ? "checks 通过" : `checks 没过: ${(t.check.failed ?? []).join(" ")}`}
        </Badge>
      )}
      {t.task && (
        <span className="text-muted-foreground/70">
          · {t.task.by === "user" ? "你派的" : "它登记的"} {ago(t.task.at)}
        </span>
      )}
      <button onClick={onEdit} className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 px-1">
        <Pencil className="size-3" />
        {t.task ? "改" : "派活"}
      </button>
    </div>
  );
}

export function TaskDialog({ t, run, onClose }: { t: TokenView | null; run: Run; onClose: () => void }) {
  const [issues, setIssues] = React.useState("");
  const [note, setNote] = React.useState("");
  const [paths, setPaths] = React.useState("");
  React.useEffect(() => {
    setIssues((t?.task?.issues ?? []).join(" "));
    setNote(t?.task?.note ?? "");
    setPaths((t?.task?.paths ?? []).join(", "));
  }, [t]);
  const nums = issues
    .split(/[\s,，#]+/)
    .filter(Boolean)
    .map(Number);
  const bad = nums.some((n) => !Number.isInteger(n) || n <= 0);
  const save = (clear: boolean) =>
    run(async () => {
      await api("PUT", `api/tokens/${t!.id}/task`, clear ? { issues: [], note: "" } : { issues: nums, note, paths: paths.trim() ? [paths] : [] });
      onClose();
    }, clear ? "已清空" : "已登记");
  return (
    <Dialog open={!!t} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t?.label} 在做什么</DialogTitle>
          <DialogDescription>填 issue 号, 它下次 hxoa status 或 hxoa task 就能看到。它自己登记的会被覆盖。</DialogDescription>
        </DialogHeader>
        <Input placeholder="issue 号, 如 104 108" value={issues} onChange={(e) => setIssues(e.target.value)} />
        <Input placeholder="一句说明 (可选)" value={note} maxLength={200} onChange={(e) => setNote(e.target.value)} />
        <Input placeholder="认领的目录 (可选), 逗号分隔, 如 client/web, protocol" value={paths} onChange={(e) => setPaths(e.target.value)} />
        <DialogFooter>
          {t?.task && (
            <Button variant="outline" onClick={() => save(true)}>
              清空
            </Button>
          )}
          <Button disabled={bad || (!nums.length && !note.trim() && !paths.trim())} onClick={() => save(false)}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function IssueBoard({ ws }: { ws: WS }) {
  return (
    <div className="grid gap-4">
      <p className="text-muted-foreground text-sm">
        按 issue 看谁在做。实心是登记过的 (hxoa task 或你在「连接」里派的), 虚线是只在未合并的提交里提到的。一个 issue 落到两个身份会标出来。
      </p>
      <IssueRows ws={ws} />
    </div>
  );
}

function IssueRows({ ws }: { ws: WS }) {
  const map = issueMap(ws);
  const title = useIssueTitles(ws, [...map.keys()]);
  const idle = ws.tokens.filter((t) => !t.task && !(t.worker?.refs ?? []).length);
  return (
    <>
      <div className="bg-card divide-y rounded-xl border">
        {map.size === 0 && <p className="text-muted-foreground px-4 py-6 text-center text-sm">还没有身份登记任务, 提交里也没提到 issue。</p>}
        {[...map.entries()].map(([n, hs]) => {
          const info = title(n);
          return (
            <div key={n} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <IssueChip n={n} repo={ws.repo} />
              <span className={cn("min-w-0 flex-1 truncate text-sm", info?.state === "closed" && "text-muted-foreground line-through")}>
                {info?.title ?? ""}
              </span>
              {hs.length > 1 && (
                <Badge variant="destructive">
                  <AlertTriangle />
                  {hs.length} 个身份
                </Badge>
              )}
              <div className="flex flex-wrap gap-1">
                {hs.map((h) => (
                  <Badge key={h.label} variant={h.claimed ? "secondary" : "outline"} className={cn(!h.claimed && "border-dashed")}>
                    <span className={cn("size-1.5 rounded-full", h.online ? "bg-success" : "bg-muted-foreground/40")} />
                    {h.label}
                  </Badge>
                ))}
              </div>
            </div>
          );
        })}
      </div>
      {idle.length > 0 && (
        <p className="text-muted-foreground flex items-center gap-1.5 text-sm">
          <ClipboardList className="size-4" />
          空闲: {idle.map((t) => t.label).join(", ")}
        </p>
      )}
    </>
  );
}

// All workspaces at once, refreshed with the panel's state poll.
export function GlobalWork({ workspaces, onOpen }: { workspaces: WS[]; onOpen: (id: string) => void }) {
  const list = workspaces.filter((w) => issueMap(w).size > 0);
  const quiet = workspaces.filter((w) => w.tokens.length > 0 && issueMap(w).size === 0);
  return (
    <section className="grid gap-6">
      <div className="grid gap-1">
        <h1 className="text-xl font-semibold">全局分工</h1>
        <p className="text-muted-foreground text-sm">每个工作区的身份各在做哪些 issue, 几秒刷新一次。实心是登记的, 虚线是只在未合并提交里提到的, 撞车标红。</p>
      </div>
      {list.length === 0 && <p className="text-muted-foreground text-sm">现在没有身份在做 issue。</p>}
      {list.map((w) => (
        <div key={w.id} className="grid gap-2">
          <div className="flex items-center gap-2">
            <button onClick={() => onOpen(w.id)} className="text-sm font-medium hover:underline">
              {w.name}
            </button>
            <span className="text-muted-foreground text-xs">{w.online} 在线 / {w.tokens.length}</span>
            {w.repo && (
              <a href={w.repo} target="_blank" rel="noreferrer" className="text-muted-foreground font-mono text-xs hover:underline">
                {w.repo.replace("https://github.com/", "")}
              </a>
            )}
          </div>
          <IssueRows ws={w} />
        </div>
      ))}
      {quiet.length > 0 && (
        <p className="text-muted-foreground text-sm">
          没在做 issue 的工作区:{" "}
          {quiet.map((w, i) => (
            <React.Fragment key={w.id}>
              {i > 0 && ", "}
              <button onClick={() => onOpen(w.id)} className="hover:text-foreground hover:underline">
                {w.name}
              </button>
            </React.Fragment>
          ))}
        </p>
      )}
    </section>
  );
}

// Issues in flight across all workspaces, for the sidebar count.
export function busyCount(workspaces: WS[]) {
  return workspaces.reduce((a, w) => a + issueMap(w).size, 0);
}

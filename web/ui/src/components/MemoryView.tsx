import * as React from "react";
import { AlertTriangle, Check, X } from "lucide-react";
import { api, type MemoryInfo, type WorkspaceView as WS } from "@/lib/api";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Mono } from "@/components/ui/mono";

// 协作记忆: 这个工作区用记忆仓库里的哪个目录, brief 长度, 经验收件箱 (采纳 / 丢弃), 最近的 hxoa say。
export default function MemoryView({ ws, run }: { ws: WS; run: Run }) {
  const [info, setInfo] = React.useState<MemoryInfo | null>(null);
  const [error, setError] = React.useState("");
  const [dir, setDir] = React.useState("");
  const load = React.useCallback(async () => {
    try {
      const r = await api<MemoryInfo>("GET", `api/workspaces/${ws.id}/memory`);
      setInfo(r);
      setDir(r.dir);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [ws.id]);
  React.useEffect(() => {
    load();
    const id = setInterval(load, 10_000);
    return () => clearInterval(id);
  }, [load]);
  const act = (fn: () => Promise<unknown>, ok: string) => run(async () => {
    await fn();
    await load();
  }, ok);
  if (error) return <p className="text-destructive text-sm">{error}</p>;
  if (!info) return <p className="text-muted-foreground text-sm">加载中</p>;
  const long = (info.brief_lines ?? 0) > (info.brief_max ?? 60);
  return (
    <div className="grid gap-5">
      <p className="text-muted-foreground text-sm leading-relaxed">
        记忆仓库 <Mono className="text-xs">{info.root}</Mono>
        {info.remote ? (
          <>
            {" "}
            同步到 <Mono className="text-xs">{info.remote}</Mono>
          </>
        ) : (
          " (只在本机, 设 HXOA_MEMORY_REMOTE 可同步到私有仓库)"
        )}
        。Agent 开工 (hxoa task) 时拿到 brief, 第一次碰某个目录时拿到 areas/ 里的注意事项, commit 前跑 checks.yml。
      </p>
      <form
        className="bg-card flex flex-wrap items-center gap-2 rounded-xl border px-4 py-3"
        onSubmit={(e) => {
          e.preventDefault();
          act(() => api("PUT", `api/workspaces/${ws.id}/memory`, { dir: dir.trim() }), dir.trim() ? "已关联" : "已取消关联");
        }}
      >
        <span className="text-sm font-medium">记忆目录</span>
        <Input list={`mem-${ws.id}`} placeholder="如 hx-aioa, 留空取消" value={dir} onChange={(e) => setDir(e.target.value)} className="max-w-60" />
        <datalist id={`mem-${ws.id}`}>
          {info.projects.map((p) => (
            <option key={p} value={p} />
          ))}
        </datalist>
        <Button type="submit" variant="outline" disabled={dir.trim() === info.dir}>
          保存
        </Button>
        {info.dir && (
          <span className="text-muted-foreground text-xs">
            brief {info.brief_lines ?? 0} 行 · checks {(info.checks ?? []).length ? (info.checks ?? []).join(", ") : "没有"}
          </span>
        )}
      </form>
      {long && (
        <div className="flex items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-sm">
          <AlertTriangle className="size-4 shrink-0 text-amber-600" />
          brief 已有 {info.brief_lines} 行, 超过 {info.brief_max} 行: 压缩一下, 细节挪进 areas/。
        </div>
      )}
      {info.dir && (
        <>
          <section className="grid gap-2">
            <h3 className="text-sm font-medium">
              经验收件箱 <Badge variant="secondary">{(info.inbox ?? []).length}</Badge>
            </h3>
            <div className="bg-card divide-y rounded-xl border">
              {(info.inbox ?? []).length === 0 && <p className="text-muted-foreground px-4 py-4 text-center text-sm">没有待审的经验。Agent 踩坑时用 hxoa learn 写进来。</p>}
              {(info.inbox ?? []).map((t, i) => (
                <div key={`${i}-${t}`} className="flex items-center gap-3 px-4 py-2.5">
                  <span className="min-w-0 flex-1 text-sm">{t}</span>
                  <Button size="sm" variant="outline" onClick={() => act(() => api("POST", `api/workspaces/${ws.id}/inbox/${i}/adopt`, {}), "已采纳进 lessons")}>
                    <Check />
                    采纳
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => act(() => api("POST", `api/workspaces/${ws.id}/inbox/${i}/drop`, {}), "已丢弃")}>
                    <X />
                    丢弃
                  </Button>
                </div>
              ))}
            </div>
          </section>
          <section className="grid gap-2">
            <h3 className="text-sm font-medium">最近的协作消息 (hxoa say)</h3>
            <div className="bg-card rounded-xl border px-4 py-3">
              {(info.threads ?? []).length === 0 ? (
                <p className="text-muted-foreground text-center text-sm">还没有消息。</p>
              ) : (
                <ul className="grid gap-1 text-sm">
                  {(info.threads ?? []).map((l, i) => (
                    <li key={i} className="break-words">
                      {l}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </section>
        </>
      )}
    </div>
  );
}

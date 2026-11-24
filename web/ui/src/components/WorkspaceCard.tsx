import * as React from "react";
import { GitBranch, GitMerge, MessageSquareText, Plus, Trash2, Users } from "lucide-react";
import { api, type TokenView, type WorkspaceView } from "@/lib/api";
import { ago, cn } from "@/lib/utils";
import type { Run } from "@/pages/Dashboard";
import { useConfirm } from "@/components/confirm";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tip } from "@/components/ui/tooltip";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export default function WorkspaceCard({
  ws,
  run,
  onPrompt,
}: {
  ws: WorkspaceView;
  run: Run;
  onPrompt: (label: string, text: string) => void;
}) {
  const confirm = useConfirm();
  const [label, setLabel] = React.useState("");

  async function setFlag(b: { workers?: boolean; exec?: boolean }) {
    if (b.workers === false) {
      const ok = await confirm({
        title: "关闭 workers?",
        description: "之后的修改会直接写到你的目录。已有的 worker 分支会保留, 记得先合并。",
        confirm: "关闭",
      });
      if (!ok) return;
    }
    run(() => api("PATCH", `api/workspaces/${ws.id}`, b));
  }

  async function addToken() {
    const l = label.trim();
    if (!l) return;
    await run(async () => {
      const r = await api<{ prompt: string }>("POST", `api/workspaces/${ws.id}/tokens`, { label: l });
      setLabel("");
      onPrompt(l, r.prompt);
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex-wrap text-base">
          {ws.name}
          <Badge variant={ws.online ? "success" : "secondary"}>
            <Users />
            {ws.online} 在线 / {ws.tokens.length}
          </Badge>
          {ws.git && (
            <Badge variant="outline" className="max-w-48 truncate font-mono">
              <GitBranch />
              {ws.branch || "无提交"}
            </Badge>
          )}
        </CardTitle>
        <CardDescription className="font-mono text-xs break-all">{ws.path}</CardDescription>
        <CardAction className="flex flex-wrap items-center justify-end gap-4">
          <Tip content={ws.git ? "每个连接用独立 git 工作树和分支" : "需要 git 仓库"}>
            <Label className="cursor-pointer">
              <Switch checked={ws.workers} disabled={!ws.git} onCheckedChange={(v) => setFlag({ workers: v })} />
              workers
            </Label>
          </Tip>
          <Label className="cursor-pointer">
            <Switch checked={ws.exec} onCheckedChange={(v) => setFlag({ exec: v })} />
            执行命令
          </Label>
          <Tip content="移除工作区">
            <Button
              variant="ghost-destructive"
              size="icon-sm"
              onClick={async () => {
                if (
                  await confirm({
                    title: `移除工作区 ${ws.name}?`,
                    description: "所有连接令牌立即失效, 目录里的文件不受影响。",
                    confirm: "移除",
                    destructive: true,
                  })
                )
                  run(() => api("DELETE", `api/workspaces/${ws.id}`), "已移除");
              }}
            >
              <Trash2 />
            </Button>
          </Tip>
        </CardAction>
      </CardHeader>
      <CardContent className="grid gap-4">
        {ws.tokens.length === 0 ? (
          <p className="text-muted-foreground text-sm">还没有连接。在下面给 AI 起个名字, 新建一个。</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>身份</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作数</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {ws.tokens.map((t) => (
                <TokenRow key={t.id} t={t} run={run} onPrompt={onPrompt} />
              ))}
            </TableBody>
          </Table>
        )}
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            addToken();
          }}
        >
          <Input placeholder="身份名, 如 hark / claude-local" value={label} onChange={(e) => setLabel(e.target.value)} className="max-w-72" />
          <Button type="submit" disabled={!label.trim()}>
            <Plus />
            新建连接
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function TokenRow({ t, run, onPrompt }: { t: TokenView; run: Run; onPrompt: (label: string, text: string) => void }) {
  const confirm = useConfirm();
  const k = t.worker;
  const ahead = Number(k?.ahead || 0);
  const canMerge = !!k?.exists && (ahead > 0 || (k.dirty ?? []).length > 0);
  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2 font-medium">
          <span className={cn("size-2 rounded-full", t.online ? "bg-success" : "bg-muted-foreground/30")} />
          {t.label}
        </div>
        {k && (
          <div className="text-muted-foreground mt-1 font-mono text-xs">
            {k.exists
              ? `${k.branch} · 领先 ${k.ahead || 0} · 落后 ${k.behind || 0}${k.dirty?.length ? ` · 未提交 ${k.dirty.length}` : ""}`
              : "首次连接时创建工作树"}
          </div>
        )}
      </TableCell>
      <TableCell>
        <div>{t.online ? "在线" : t.bye ? "已断开" : "离线"}</div>
        <div className="text-muted-foreground text-xs">
          {ago(t.last_seen)}
          {t.last_ip && ` · ${t.last_ip}`}
        </div>
      </TableCell>
      <TableCell className="text-right tabular-nums">{t.ops}</TableCell>
      <TableCell>
        <div className="flex justify-end gap-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              run(async () => {
                const r = await api<{ prompt: string }>("GET", `api/tokens/${t.id}/prompt`);
                onPrompt(t.label, r.prompt);
              })
            }
          >
            <MessageSquareText />
            连接提示
          </Button>
          {k && (
            <Button
              size="sm"
              variant="outline"
              disabled={!canMerge}
              onClick={async () => {
                if (await confirm({ title: `合并 ${t.label} 的改动?`, description: `把 ${k.branch} 合并到你当前的分支。`, confirm: "合并" }))
                  run(() => api("POST", `api/tokens/${t.id}/merge`), `${t.label} 的改动已合并`);
              }}
            >
              <GitMerge />
              合并
            </Button>
          )}
          <Tip content="删除连接">
            <Button
              size="icon-sm"
              variant="ghost-destructive"
              onClick={async () => {
                if (
                  await confirm({
                    title: `删除连接 ${t.label}?`,
                    description: "它会立刻失去访问权。没合并的 worker 分支会保留。",
                    confirm: "删除",
                    destructive: true,
                  })
                )
                  run(() => api("DELETE", `api/tokens/${t.id}`), "已删除");
              }}
            >
              <Trash2 />
            </Button>
          </Tip>
        </div>
      </TableCell>
    </TableRow>
  );
}

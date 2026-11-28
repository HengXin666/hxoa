import * as React from "react";
import { AlertTriangle, ClipboardList, GitBranch, GitMerge, History, MessageSquareText, Plus, Settings2, Trash2, Users } from "lucide-react";
import { api, type Op, type TokenView, type WorkspaceView as WS } from "@/lib/api";
import { ago, cn } from "@/lib/utils";
import type { Run } from "@/pages/Dashboard";
import { useConfirm } from "@/components/confirm";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Mono } from "@/components/ui/mono";
import { Tip } from "@/components/ui/tooltip";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import OpsTable from "@/components/OpsCard";
import { IssueBoard, TaskDialog, TokenIssues } from "@/components/IssueWork";

export default function WorkspaceView({
  ws,
  ops,
  publicReady,
  run,
  onPrompt,
  onOpenNet,
}: {
  ws: WS;
  ops: Op[];
  publicReady: boolean;
  run: Run;
  onPrompt: (label: string, text: string) => void;
  onOpenNet: () => void;
}) {
  return (
    <section className="grid gap-5">
      <div className="grid gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-xl font-semibold">{ws.name}</h1>
          <Badge variant={ws.online ? "success" : "secondary"}>
            <Users />
            {ws.online} 在线 / {ws.tokens.length}
          </Badge>
          {ws.git && (
            <Badge variant="outline">
              <GitBranch />
              <Mono className="text-xs">{ws.branch || "无提交"}</Mono>
            </Badge>
          )}
          {ws.workers && <Badge variant="secondary">并行编辑</Badge>}
        </div>
        <Mono className="text-muted-foreground break-all">{ws.path}</Mono>
      </div>

      {!publicReady && (
        <button
          onClick={onOpenNet}
          className="flex cursor-pointer items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-4 py-2.5 text-left text-sm"
        >
          <AlertTriangle className="size-4 shrink-0 text-amber-600" />
          公网地址还没就绪, AI 暂时连不进来。点这里查看公网访问。
        </button>
      )}

      <Tabs defaultValue="tokens">
        <TabsList>
          <TabsTrigger value="tokens">
            <Users />
            连接
          </TabsTrigger>
          <TabsTrigger value="issues">
            <ClipboardList />
            分工
          </TabsTrigger>
          <TabsTrigger value="settings">
            <Settings2 />
            设置
          </TabsTrigger>
          <TabsTrigger value="ops">
            <History />
            记录
          </TabsTrigger>
        </TabsList>
        <TabsContent value="tokens">
          <Tokens ws={ws} run={run} onPrompt={onPrompt} />
        </TabsContent>
        <TabsContent value="issues">
          <IssueBoard ws={ws} />
        </TabsContent>
        <TabsContent value="settings">
          <SettingsPane ws={ws} run={run} />
        </TabsContent>
        <TabsContent value="ops">
          <OpsTable ops={ops} hideWorkspace />
        </TabsContent>
      </Tabs>
    </section>
  );
}

function Tokens({ ws, run, onPrompt }: { ws: WS; run: Run; onPrompt: (label: string, text: string) => void }) {
  const [label, setLabel] = React.useState("");
  const [editing, setEditing] = React.useState<TokenView | null>(null);
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
    <div className="grid gap-4">
      <p className="text-muted-foreground text-sm">每个连接是一个身份 (一个 AI 或一台机器), 有自己的令牌。删掉它, 那个身份立刻断开。</p>
      <div className="bg-card divide-y rounded-xl border">
        {ws.tokens.length === 0 && <p className="text-muted-foreground px-4 py-6 text-center text-sm">还没有连接。在下面给 AI 起个名字, 新建一个。</p>}
        {ws.tokens.map((t) => (
          <TokenRow key={t.id} t={t} repo={ws.repo} run={run} onPrompt={onPrompt} onEdit={() => setEditing(t)} />
        ))}
        <form
          className="flex gap-2 px-4 py-3"
          onSubmit={(e) => {
            e.preventDefault();
            addToken();
          }}
        >
          <Input placeholder="新身份的名字, 如 hark / claude-local" value={label} onChange={(e) => setLabel(e.target.value)} className="max-w-80" />
          <Button type="submit" disabled={!label.trim()}>
            <Plus />
            新建连接
          </Button>
        </form>
      </div>
      <TaskDialog t={editing} run={run} onClose={() => setEditing(null)} />
    </div>
  );
}

function TokenRow({
  t,
  repo,
  run,
  onPrompt,
  onEdit,
}: {
  t: TokenView;
  repo?: string;
  run: Run;
  onPrompt: (label: string, text: string) => void;
  onEdit: () => void;
}) {
  const confirm = useConfirm();
  const k = t.worker;
  const canMerge = !!k?.exists && (Number(k.ahead || 0) > 0 || (k.dirty ?? []).length > 0);
  return (
    <div className="flex flex-wrap items-center gap-4 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-sm font-medium">
          <span className={cn("size-2 shrink-0 rounded-full", t.online ? "bg-success" : "bg-muted-foreground/30")} />
          {t.label}
          <span className="text-muted-foreground text-xs font-normal">
            {t.online ? "在线" : t.bye ? "已断开" : "离线"} · {ago(t.last_seen)}
            {t.last_ip && " · "}
            {t.last_ip && <Mono className="text-xs">{t.last_ip}</Mono>}
            {" · "}
            {t.ops} 次操作
          </span>
        </div>
        {k && (
          <div className="text-muted-foreground mt-1 pl-4 text-xs">
            {k.exists ? (
              <>
                <Mono className="text-xs">{k.branch}</Mono> · 领先 {k.ahead || 0} · 落后 {k.behind || 0}
                {k.dirty?.length ? ` · 未提交 ${k.dirty.length}` : ""}
              </>
            ) : (
              "首次连接时创建工作树"
            )}
          </div>
        )}
        <TokenIssues t={t} repo={repo} onEdit={onEdit} />
      </div>
      <div className="flex gap-1">
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
    </div>
  );
}

function SettingsPane({ ws, run }: { ws: WS; run: Run }) {
  const confirm = useConfirm();
  async function setFlag(b: { workers?: boolean; exec?: boolean }) {
    if (b.workers === false) {
      const ok = await confirm({
        title: "关闭并行编辑?",
        description: "之后的修改会直接写到你的目录。已有的 worker 分支会保留, 记得先合并。",
        confirm: "关闭",
      });
      if (!ok) return;
    }
    run(() => api("PATCH", `api/workspaces/${ws.id}`, b));
  }
  return (
    <div className="grid gap-5">
      <div className="bg-card divide-y rounded-xl border">
        <SettingRow title="允许执行命令" desc="AI 可以在这个目录里跑 shell 命令 (编译、测试、git)。关掉后只能读写文件。">
          <Switch checked={ws.exec} onCheckedChange={(v) => setFlag({ exec: v })} />
        </SettingRow>
        <SettingRow
          title="并行编辑 (workers)"
          desc={
            ws.git
              ? "每个连接在自己的 git 工作树和分支里改, 互不撞车, 做完由它自己处理冲突并合入主线 (也可以在「连接」里一键合并)。只让一个 AI 改时关掉, 改动直接出现在你的目录。"
              : "需要这个目录是 git 仓库。"
          }
        >
          <Switch checked={ws.workers} disabled={!ws.git} onCheckedChange={(v) => setFlag({ workers: v })} />
        </SettingRow>
      </div>
      <div className="border-destructive/30 rounded-xl border">
        <SettingRow title="移除工作区" desc="所有连接令牌立即失效。目录里的文件不受影响。">
          <Button
            variant="outline"
            className="text-destructive"
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
            移除
          </Button>
        </SettingRow>
      </div>
    </div>
  );
}

function SettingRow({ title, desc, children }: { title: string; desc: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-6 px-4 py-4">
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{title}</div>
        <p className="text-muted-foreground mt-1 text-sm leading-relaxed">{desc}</p>
      </div>
      {children}
    </div>
  );
}

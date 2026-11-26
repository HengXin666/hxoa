import * as React from "react";
import { Copy, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type AdminToken } from "@/lib/api";
import { copyText } from "@/lib/utils";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Mono } from "@/components/ui/mono";
import { useConfirm } from "@/components/confirm";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

function ago(s?: string) {
  if (!s || s.startsWith("0001")) return "从未使用";
  const d = (Date.now() - new Date(s).getTime()) / 1000;
  if (d < 60) return "刚刚";
  if (d < 3600) return `${Math.floor(d / 60)} 分钟前`;
  if (d < 86400) return `${Math.floor(d / 3600)} 小时前`;
  return `${Math.floor(d / 86400)} 天前`;
}

// 远程管理: 管理令牌让别的 App (如 HX-AIOA) 经公网地址管理工作区与连接令牌
export default function RemoteAdminView({ run, publicUrl }: { run: Run; publicUrl: string }) {
  const [tokens, setTokens] = React.useState<AdminToken[]>([]);
  const [label, setLabel] = React.useState("aioa");
  const [created, setCreated] = React.useState<{ label: string; secret: string; base: string } | null>(null);
  const confirm = useConfirm();

  const load = React.useCallback(async () => {
    try {
      setTokens((await api<{ tokens: AdminToken[] }>("GET", "api/admin-tokens")).tokens);
    } catch (e) {
      toast.error((e as Error).message);
    }
  }, []);
  React.useEffect(() => {
    load();
  }, [load]);

  async function create() {
    const ok = await confirm({
      title: "创建管理令牌?",
      description: "拿到它的人能经公网地址在这台机器上把任意目录加成工作区, 发连接令牌, 进而读写文件和执行命令。只交给你信任的 App, 不用了就删除。",
      confirm: "我明白, 创建",
      destructive: true,
    });
    if (!ok) return;
    await run(async () => {
      const r = await api<{ label: string; secret: string; base: string }>("POST", "api/admin-tokens", { label: label.trim() });
      setCreated(r);
      await load();
    });
  }

  async function remove(t: AdminToken) {
    if (!(await confirm({ title: `删除管理令牌 ${t.label}?`, description: "用它的 App 会立刻失去管理权限。", confirm: "删除", destructive: true }))) return;
    await run(async () => {
      await api("DELETE", `api/admin-tokens/${t.id}`);
      await load();
    }, "已删除");
  }

  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">远程管理</h1>
      <div className="bg-card flex gap-3 rounded-xl border border-amber-500/40 p-5 text-sm">
        <ShieldAlert className="size-5 shrink-0 text-amber-600" />
        <div className="grid gap-1">
          <div className="font-medium">默认关闭, 有管理令牌才开放</div>
          <div className="text-muted-foreground">
            管理令牌可以经公网地址 <Mono>{publicUrl || "(隧道未就绪)"}/v1/admin/</Mono> 管理工作区, 连接令牌, 目录选择和合并, 等于这台机器的远程控制权。不能远程创建管理令牌, 也不能改隧道和面板账号。连续输错 5 次会按 IP 锁定。
          </div>
        </div>
      </div>

      <div className="bg-card grid gap-4 rounded-xl border p-5">
        <div className="flex items-center gap-2">
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="名字, 如 aioa" className="max-w-56" />
          <Button onClick={create} disabled={!label.trim()}>
            <Plus />
            新建管理令牌
          </Button>
        </div>
        {tokens.length === 0 ? (
          <div className="text-muted-foreground text-sm">还没有管理令牌, 远程管理处于关闭状态。</div>
        ) : (
          <div className="divide-y">
            {tokens.map((t) => (
              <div key={t.id} className="flex items-center gap-3 py-3 text-sm">
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{t.label}</div>
                  <div className="text-muted-foreground text-xs">
                    <Mono>{t.hint}…</Mono> · {ago(t.last_seen)}
                    {t.last_ip ? ` · ${t.last_ip}` : ""} · {t.ops} 次
                  </div>
                </div>
                <Button size="icon-sm" variant="ghost" onClick={() => remove(t)} title="删除">
                  <Trash2 />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      <Dialog open={!!created} onOpenChange={(o) => !o && setCreated(null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>管理令牌 {created?.label}</DialogTitle>
            <DialogDescription>只显示这一次, 关掉后无法再查看。在 App 里填 "地址 + 管理令牌"。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2 text-sm">
            <div className="text-muted-foreground text-xs">地址</div>
            <pre className="bg-muted rounded-lg p-3 font-mono text-xs break-all whitespace-pre-wrap">{created?.base || "(隧道未就绪)"}</pre>
            <div className="text-muted-foreground text-xs">管理令牌</div>
            <pre className="bg-muted rounded-lg p-3 font-mono text-xs break-all whitespace-pre-wrap">{created?.secret}</pre>
          </div>
          <DialogFooter>
            <Button
              onClick={async () => {
                await copyText(created?.secret ?? "");
                toast.success("已复制令牌");
              }}
            >
              <Copy />
              复制令牌
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

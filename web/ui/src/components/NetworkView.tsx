import * as React from "react";
import { CheckCircle2, ChevronDown, Cloud, Copy, Loader2, Power, Stethoscope, XCircle, Zap } from "lucide-react";
import { toast } from "sonner";
import { api, type CheckStep, type Tunnel } from "@/lib/api";
import { cn, copyText } from "@/lib/utils";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Mono } from "@/components/ui/mono";

export type Health = { tone: "ok" | "wait" | "bad" | "off"; title: string };

export function tunnelHealth(t: Tunnel, publicUrl: string): Health {
  if (t.fixed || t.mode === "external") return publicUrl ? { tone: "ok", title: "固定地址" } : { tone: "bad", title: "未配置" };
  if (t.mode === "off") return { tone: "off", title: "未开启" };
  if (t.state === "running" && publicUrl) return { tone: "ok", title: "已连通" };
  if (t.state === "error") return { tone: "bad", title: "连接出错" };
  if (t.state === "downloading") return { tone: "wait", title: "正在下载 cloudflared" };
  return { tone: "wait", title: "正在连接 Cloudflare" };
}

const MODE_TEXT: Record<string, string> = { quick: "快速隧道", token: "固定域名 (Cloudflare 隧道)", external: "外部 cloudflared", off: "未开启" };

export default function NetworkView({ tunnel: t, publicUrl, run }: { tunnel: Tunnel; publicUrl: string; run: Run }) {
  const h = tunnelHealth(t, publicUrl);
  const fixed = !!t.fixed || t.mode === "external";
  const [check, setCheck] = React.useState<{ healthy: boolean; steps: CheckStep[] } | null>(null);
  const [checking, setChecking] = React.useState(false);
  const [setup, setSetup] = React.useState(false);
  const [logs, setLogs] = React.useState(false);

  async function runCheck() {
    setChecking(true);
    try {
      setCheck(await api("POST", "api/tunnel/check"));
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setChecking(false);
    }
  }

  const sub = fixed
    ? "地址由部署配置固定 (反向代理或命名隧道), AI 用它连进来, 重启也不会变。"
    : t.mode === "off"
      ? "还没开启。开一个隧道, AI 就能从外面连到这台机器。cloudflared 不用自己装。"
      : t.mode === "quick"
        ? "快速隧道免账号, 但 hxoad 每次重启地址都会变, 连接提示要重发。长期用请换固定域名。"
        : "固定域名, 重启不变。";

  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">公网访问</h1>

      <div className="bg-card grid gap-4 rounded-xl border p-5">
        <div className="flex items-start gap-3">
          <StatusIcon tone={h.tone} />
          <div className="min-w-0 flex-1">
            <div className="font-medium">{h.title}</div>
            {publicUrl ? (
              <div className="mt-1 flex items-center gap-1">
                <Mono className="truncate">{publicUrl}</Mono>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  title="复制"
                  onClick={async () => {
                    await copyText(publicUrl);
                    toast.success("已复制");
                  }}
                >
                  <Copy />
                </Button>
              </div>
            ) : (
              <div className="text-muted-foreground mt-1 text-sm">还没有公网地址</div>
            )}
          </div>
          <Button variant="outline" onClick={runCheck} disabled={checking}>
            {checking ? <Loader2 className="animate-spin" /> : <Stethoscope />}
            检查连通性
          </Button>
        </div>

        {t.hint ? (
          <p className="rounded-md bg-amber-500/10 px-3 py-2 text-sm text-amber-800 dark:text-amber-300">{t.hint}</p>
        ) : t.error && h.tone === "bad" ? (
          <p className="text-destructive text-sm">{t.error}</p>
        ) : (
          <p className="text-muted-foreground text-sm">{sub}</p>
        )}

        {check && (
          <ol className="grid gap-2 border-t pt-4">
            {check.steps.map((s) => (
              <li key={s.name} className="flex items-start gap-2.5 text-sm">
                {s.ok ? <CheckCircle2 className="text-success mt-0.5 size-4 shrink-0" /> : <XCircle className="text-destructive mt-0.5 size-4 shrink-0" />}
                <span className="w-20 shrink-0 font-medium">{s.name}</span>
                <span className={cn("min-w-0 flex-1", !s.ok && "text-destructive")}>{s.detail}</span>
                {s.ok && s.ms ? <span className="text-muted-foreground text-xs tabular-nums">{s.ms} ms</span> : null}
              </li>
            ))}
            {check.healthy && <li className="text-success pl-6 text-sm">一切正常, AI 可以用连接提示连进来。</li>}
          </ol>
        )}

        <dl className="text-muted-foreground grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 border-t pt-4 text-sm">
          <dt>方式</dt>
          <dd className="text-foreground">{fixed ? "固定地址" : (MODE_TEXT[t.mode] ?? t.mode)}</dd>
          {t.network && (
            <>
              <dt>线路</dt>
              <dd className="text-foreground">{t.network}</dd>
            </>
          )}
        </dl>
      </div>

      {!fixed && (
        <div className="grid gap-3">
          <h2 className="text-sm font-medium">换一种方式</h2>
          <div className="grid gap-3 sm:grid-cols-3">
            <ModeCard
              active={t.mode === "quick"}
              icon={<Zap />}
              title="快速隧道"
              desc="免账号, 一键开。地址随重启变化。"
              onClick={() => run(() => api("POST", "api/tunnel/mode", { mode: "quick" }), "已开启快速隧道")}
            />
            <ModeCard
              active={t.mode === "token" || setup}
              icon={<Cloud />}
              title="固定域名"
              desc="用你在 Cloudflare 的域名, 地址永久不变。"
              onClick={() => setSetup(!setup)}
            />
            <ModeCard
              active={t.mode === "off"}
              icon={<Power />}
              title="关闭"
              desc="只在本机或局域网用。"
              onClick={() => run(() => api("POST", "api/tunnel/mode", { mode: "off" }), "隧道已关闭")}
            />
          </div>
          {setup && <FixedDomainForm run={run} onDone={() => setSetup(false)} />}
        </div>
      )}

      {t.logs && t.logs.length > 0 && (
        <div className="grid gap-2">
          <button onClick={() => setLogs(!logs)} className="text-muted-foreground hover:text-foreground flex w-fit cursor-pointer items-center gap-1 text-xs">
            <ChevronDown className={cn("size-3.5 transition-transform", !logs && "-rotate-90")} />
            cloudflared 原始日志 (排查用, 平时不用看)
          </button>
          {logs && (
            <pre className="bg-muted max-h-80 overflow-auto rounded-lg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">{t.logs.join("\n")}</pre>
          )}
        </div>
      )}
    </section>
  );
}

function StatusIcon({ tone }: { tone: Health["tone"] }) {
  const cls = "mt-0.5 size-5 shrink-0";
  if (tone === "ok") return <CheckCircle2 className={cn(cls, "text-success")} />;
  if (tone === "bad") return <XCircle className={cn(cls, "text-destructive")} />;
  if (tone === "wait") return <Loader2 className={cn(cls, "animate-spin text-amber-600")} />;
  return <Power className={cn(cls, "text-muted-foreground")} />;
}

function ModeCard({ active, icon, title, desc, onClick }: { active: boolean; icon: React.ReactNode; title: string; desc: string; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "bg-card grid cursor-pointer gap-1 rounded-xl border p-4 text-left transition-colors [&_svg]:size-4",
        active ? "border-foreground ring-1 ring-foreground" : "hover:bg-accent",
      )}
    >
      <span className="flex items-center gap-2 text-sm font-medium">
        {icon}
        {title}
      </span>
      <span className="text-muted-foreground text-xs leading-relaxed">{desc}</span>
    </button>
  );
}

function FixedDomainForm({ run, onDone }: { run: Run; onDone: () => void }) {
  const [how, setHow] = React.useState<"auto" | "paste">("auto");
  const [host, setHost] = React.useState("");
  const [tok, setTok] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  async function submit() {
    setBusy(true);
    await run(async () => {
      if (how === "auto") await api("POST", "api/tunnel/auto", { hostname: host, api_token: tok });
      else await api("POST", "api/tunnel/token", { hostname: host, token: tok });
      setTok("");
      onDone();
    }, "固定域名已配置");
    setBusy(false);
  }
  return (
    <div className="bg-card grid gap-4 rounded-xl border p-5">
      <div className="flex gap-1">
        <Button size="sm" variant={how === "auto" ? "secondary" : "ghost"} onClick={() => setHow("auto")}>
          用 API 令牌自动配置
        </Button>
        <Button size="sm" variant={how === "paste" ? "secondary" : "ghost"} onClick={() => setHow("paste")}>
          我已有隧道 token
        </Button>
      </div>
      <p className="text-muted-foreground text-sm leading-relaxed">
        {how === "auto"
          ? "在 Cloudflare 的 My Profile → API Tokens 建一个令牌, 权限给 Cloudflare Tunnel 编辑、DNS 编辑、Zone 读取。hxoad 自动建隧道、写 DNS, 令牌只用这一次, 不保存。"
          : "在 Zero Trust → Networks → Tunnels 新建隧道, Public hostname 指向 http://127.0.0.1:7070, 把安装命令或其中的 token 粘贴进来。"}
      </p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Input placeholder="域名, 如 oa.example.com" value={host} onChange={(e) => setHost(e.target.value)} className="sm:max-w-64" />
        <Input
          type={how === "auto" ? "password" : "text"}
          placeholder={how === "auto" ? "Cloudflare API 令牌" : "cloudflared ... --token eyJ... 或只粘 token"}
          value={tok}
          onChange={(e) => setTok(e.target.value)}
        />
        <Button onClick={submit} disabled={busy || !tok || (how === "auto" && !host)}>
          {busy && <Loader2 className="animate-spin" />}
          {how === "auto" ? "一键配置" : "保存并启动"}
        </Button>
      </div>
    </div>
  );
}

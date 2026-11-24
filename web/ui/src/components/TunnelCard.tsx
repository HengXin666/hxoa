import * as React from "react";
import { Cloud, Globe, Loader2, Power, Zap } from "lucide-react";
import { api, type Tunnel } from "@/lib/api";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

const MODES: Record<string, string> = { off: "未开启", quick: "快速隧道", token: "固定域名", external: "外部地址" };
const STATES: Record<string, string> = { stopped: "已停止", downloading: "下载 cloudflared", starting: "连接中", running: "运行中", error: "出错" };

export default function TunnelCard({ tunnel: t, publicUrl, run }: { tunnel: Tunnel; publicUrl: string; run: Run }) {
  const [form, setForm] = React.useState<"" | "auto" | "paste">("");
  const [host, setHost] = React.useState("");
  const [tok, setTok] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const live = t.state === "running" && !!t.url;
  const fixed = t.mode === "external" || t.fixed;

  const hint = t.error
    ? null
    : fixed
      ? "地址由部署配置固定 (反向代理或外部 cloudflared), 这里不用改。"
      : t.mode === "off"
        ? "还没开启。选一种方式, AI 就能通过公网地址连到这台机器。cloudflared 不用自己装, 需要时自动下载。"
        : t.mode === "quick"
          ? "快速隧道免账号, 但 hxoad 重启后地址会变, 要重新复制连接提示。想要固定地址用下面的固定域名。"
          : "固定地址, 重启不变。";

  async function submit() {
    setBusy(true);
    await run(async () => {
      if (form === "auto") await api("POST", "api/tunnel/auto", { hostname: host, api_token: tok });
      else await api("POST", "api/tunnel/token", { hostname: host, token: tok });
      setTok("");
      setForm("");
    }, "隧道已配置");
    setBusy(false);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <Globe className="size-4" />
          公网访问
          {fixed ? (
            <Badge variant="success">固定地址</Badge>
          ) : (
            <Badge variant={live ? "success" : "secondary"}>
              {MODES[t.mode] ?? t.mode}
              {t.mode !== "off" && ` · ${STATES[t.state] ?? t.state}`}
            </Badge>
          )}
        </CardTitle>
        <CardDescription className="font-mono text-xs break-all">{t.url || publicUrl || "AI 通过这个地址连到 hxoad"}</CardDescription>
        {!fixed && (
          <CardAction className="flex flex-wrap justify-end gap-2">
            <Button size="sm" variant="outline" onClick={() => run(() => api("POST", "api/tunnel/mode", { mode: "quick" }))}>
              <Zap />
              快速隧道
            </Button>
            <Button size="sm" variant={form === "auto" ? "secondary" : "outline"} onClick={() => setForm(form === "auto" ? "" : "auto")}>
              <Cloud />
              固定域名
            </Button>
            {t.mode !== "off" && (
              <Button size="sm" variant="ghost-destructive" onClick={() => run(() => api("POST", "api/tunnel/mode", { mode: "off" }))}>
                <Power />
                关闭
              </Button>
            )}
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="grid gap-3">
        {t.error ? <p className="text-destructive text-sm">{t.error}</p> : <p className="text-muted-foreground text-sm">{hint}</p>}
        {form && (
          <div className="bg-muted/50 grid gap-3 rounded-lg border p-4">
            <div className="flex gap-2 text-sm">
              <Button size="sm" variant={form === "auto" ? "default" : "ghost"} onClick={() => setForm("auto")}>
                用 API 令牌自动配置
              </Button>
              <Button size="sm" variant={form === "paste" ? "default" : "ghost"} onClick={() => setForm("paste")}>
                粘贴隧道 token
              </Button>
            </div>
            <p className="text-muted-foreground text-xs leading-relaxed">
              {form === "auto"
                ? "Cloudflare → My Profile → API Tokens 创建令牌, 权限: Account · Cloudflare Tunnel · Edit, Zone · DNS · Edit, Zone · Zone · Read。hxoad 会自动建隧道、写 DNS、启动 cloudflared; API 令牌只用这一次, 不保存。"
                : "Zero Trust → Networks → Tunnels 新建隧道, Public hostname 指向 http://127.0.0.1:7070, 把安装命令 (或其中的 token) 粘贴到这里。"}
            </p>
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input placeholder="域名, 如 oa.example.com" value={host} onChange={(e) => setHost(e.target.value)} className="sm:max-w-64" />
              <Input
                type={form === "auto" ? "password" : "text"}
                placeholder={form === "auto" ? "Cloudflare API 令牌" : "cloudflared ... --token eyJ... 或只粘 token"}
                value={tok}
                onChange={(e) => setTok(e.target.value)}
              />
              <Button onClick={submit} disabled={busy || !tok}>
                {busy && <Loader2 className="animate-spin" />}
                {form === "auto" ? "一键配置" : "保存并启动"}
              </Button>
            </div>
          </div>
        )}
        {!fixed && t.logs && t.logs.length > 0 && (
          <details className="text-sm">
            <summary className="text-muted-foreground cursor-pointer text-xs">cloudflared 日志</summary>
            <pre className="bg-muted mt-2 max-h-60 overflow-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap">{t.logs.join("\n")}</pre>
          </details>
        )}
      </CardContent>
    </Card>
  );
}

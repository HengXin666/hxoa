import * as React from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api, type AuthInfo } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export default function AccountDialog({
  open,
  onOpenChange,
  auth,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  auth: AuthInfo;
  onSaved: () => void;
}) {
  const [username, setUsername] = React.useState("");
  const [oldPw, setOldPw] = React.useState("");
  const [pw, setPw] = React.useState("");
  const [pw2, setPw2] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    if (open) {
      setUsername(auth.username || "");
      setOldPw("");
      setPw("");
      setPw2("");
      setErr("");
    }
  }, [open, auth.username]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (pw !== pw2) return setErr("两次输入的新密码不一样");
    if (pw.length < 8) return setErr("新密码至少 8 位");
    setBusy(true);
    setErr("");
    try {
      await api("POST", "api/account", { username, old_password: oldPw, new_password: pw });
      toast.success("已保存。其它地方登录的会话都已失效");
      onOpenChange(false);
      onSaved();
    } catch (e: any) {
      setErr(e.message);
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{auth.has_account ? "修改账号密码" : "设置账号密码"}</DialogTitle>
          <DialogDescription>保存后, 除了当前这个浏览器, 其它已登录的地方都要重新登录。</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="acc-u">用户名</Label>
            <Input id="acc-u" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
          </div>
          {auth.has_account && (
            <div className="grid gap-2">
              <Label htmlFor="acc-o">当前密码</Label>
              <Input id="acc-o" type="password" autoComplete="current-password" value={oldPw} onChange={(e) => setOldPw(e.target.value)} required />
            </div>
          )}
          <div className="grid gap-2">
            <Label htmlFor="acc-n">新密码</Label>
            <Input id="acc-n" type="password" autoComplete="new-password" value={pw} onChange={(e) => setPw(e.target.value)} required />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="acc-n2">再输一次新密码</Label>
            <Input id="acc-n2" type="password" autoComplete="new-password" value={pw2} onChange={(e) => setPw2(e.target.value)} required />
          </div>
          {err && <p className="text-destructive text-sm">{err}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={busy}>
              {busy && <Loader2 className="animate-spin" />}保存
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

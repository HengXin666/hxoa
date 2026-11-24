import * as React from "react";
import { ArrowUp, Folder, FolderOpen, FolderPlus, GitBranch } from "lucide-react";
import { api } from "@/lib/api";
import type { Run } from "@/pages/Dashboard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tip } from "@/components/ui/tooltip";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

interface Browse {
  path: string;
  parent: string;
  dirs: string[];
  git: boolean;
}

export default function AddWorkspace({ run }: { run: Run }) {
  const [path, setPath] = React.useState("");
  const [name, setName] = React.useState("");
  const [workers, setWorkers] = React.useState(false);
  const [exec, setExec] = React.useState(true);
  const [pick, setPick] = React.useState<Browse | null>(null);

  async function browse(p: string) {
    try {
      const r = await api<Browse>("GET", "api/browse?path=" + encodeURIComponent(p));
      setPick(r);
      setPath(r.path);
    } catch (e) {
      await run(() => Promise.reject(e));
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <FolderPlus className="size-4" />
          添加工作区
        </CardTitle>
        <CardDescription>选一个目录分享出去, 每个连接者单独发令牌。</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="grid gap-3 md:grid-cols-[1fr_200px_auto]">
          <div className="flex gap-2">
            <Input placeholder="目录, 如 ~/code/project" value={path} onChange={(e) => setPath(e.target.value)} className="font-mono" />
            <Button variant="outline" onClick={() => (pick ? setPick(null) : browse(path))}>
              <FolderOpen />
              {pick ? "收起" : "浏览"}
            </Button>
          </div>
          <Input placeholder="名字 (可选)" value={name} onChange={(e) => setName(e.target.value)} />
          <Button
            disabled={!path.trim()}
            onClick={() =>
              run(async () => {
                await api("POST", "api/workspaces", { path, name, workers, exec });
                setPath("");
                setName("");
                setPick(null);
              }, "已添加工作区")
            }
          >
            添加
          </Button>
        </div>
        {pick && (
          <div className="overflow-hidden rounded-lg border">
            <div className="bg-muted/50 flex items-center gap-2 border-b px-3 py-2 font-mono text-xs">
              <span className="truncate">{pick.path}</span>
              {pick.git && (
                <span className="text-success ml-auto flex shrink-0 items-center gap-1">
                  <GitBranch className="size-3" />
                  git 仓库
                </span>
              )}
            </div>
            <div className="max-h-64 overflow-auto py-1">
              <button className="hover:bg-accent flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm" onClick={() => browse(pick.parent)}>
                <ArrowUp className="text-muted-foreground size-4" />
                ..
              </button>
              {pick.dirs.map((d) => (
                <button
                  key={d}
                  className="hover:bg-accent flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm"
                  onClick={() => browse(pick.path.replace(/\/$/, "") + "/" + d)}
                >
                  <Folder className="text-muted-foreground size-4" />
                  {d}
                </button>
              ))}
              {pick.dirs.length === 0 && <p className="text-muted-foreground px-3 py-1.5 text-sm">没有子目录</p>}
            </div>
          </div>
        )}
        <div className="flex flex-wrap gap-6">
          <Tip content="每个连接在自己的 git 工作树和分支里改, 互不撞车, 你在这里一键合并。只让一个 AI 改时关掉, 改动直接出现在你的目录里。">
            <Label className="cursor-pointer">
              <Switch checked={workers} onCheckedChange={setWorkers} />
              workers (并行编辑)
            </Label>
          </Tip>
          <Label className="cursor-pointer">
            <Switch checked={exec} onCheckedChange={setExec} />
            允许执行命令
          </Label>
        </div>
      </CardContent>
    </Card>
  );
}

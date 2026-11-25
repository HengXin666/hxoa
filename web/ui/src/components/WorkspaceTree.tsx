import * as React from "react";
import { ChevronRight, FolderClosed } from "lucide-react";
import type { WorkspaceView } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Mono } from "@/components/ui/mono";

// Workspaces grouped by the folders they share. Folders that hold a single
// child are folded into one row (like VS Code's compact folders), so
// /home/hx/code/a and /home/hx/code/b show as "/home/hx/code" with a and b
// under it, while a lone workspace keeps its whole path on one row.

interface Node {
  label: string; // path segment(s) this row stands for
  path: string;
  ws?: WorkspaceView; // set when this folder is itself a workspace
  children: Node[];
}

function build(list: WorkspaceView[]): Node[] {
  const root: Node = { label: "", path: "", children: [] };
  for (const w of list) {
    const segs = w.path.split("/").filter(Boolean);
    let cur = root;
    let p = "";
    for (const s of segs) {
      p += "/" + s;
      let next = cur.children.find((c) => c.path === p);
      if (!next) {
        next = { label: s, path: p, children: [] };
        cur.children.push(next);
      }
      cur = next;
    }
    if (segs.length === 0) cur = root;
    cur.ws = w;
  }
  const compact = (n: Node): Node => {
    n.children = n.children.map(compact);
    // fold a plain folder with exactly one child into that child
    while (!n.ws && n.children.length === 1) {
      const c = n.children[0];
      n = { ...c, label: (n.label ? n.label + "/" : "") + c.label };
    }
    return n;
  };
  const tops = root.children.map(compact).map((n) => ({ ...n, label: "/" + n.label }));
  const sort = (ns: Node[]) => {
    ns.sort((a, b) => a.label.localeCompare(b.label));
    ns.forEach((n) => sort(n.children));
  };
  sort(tops);
  return tops;
}

export default function WorkspaceTree({
  workspaces,
  selected,
  onSelect,
}: {
  workspaces: WorkspaceView[];
  selected?: string;
  onSelect: (id: string) => void;
}) {
  const tree = React.useMemo(() => build(workspaces), [workspaces]);
  const [closed, setClosed] = React.useState<Set<string>>(() => new Set(JSON.parse(localStorage.getItem("hxoa.tree.closed") || "[]")));
  const toggle = (p: string) =>
    setClosed((s) => {
      const n = new Set(s);
      if (n.has(p)) n.delete(p);
      else n.add(p);
      localStorage.setItem("hxoa.tree.closed", JSON.stringify([...n]));
      return n;
    });

  const render = (n: Node, depth: number): React.ReactNode => {
    const hasKids = n.children.length > 0;
    const open = !closed.has(n.path);
    const pad = { paddingLeft: 8 + depth * 14 };
    const chevron = hasKids ? (
      <span
        role="button"
        aria-label={open ? "折叠" : "展开"}
        onClick={(e) => {
          e.stopPropagation();
          toggle(n.path);
        }}
        className="text-muted-foreground hover:text-foreground -m-1 flex size-6 shrink-0 cursor-pointer items-center justify-center rounded"
      >
        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
      </span>
    ) : (
      <span className="size-4 shrink-0" />
    );
    const row = n.ws ? (
      <button
        key={n.path}
        onClick={() => onSelect(n.ws!.id)}
        title={n.path}
        style={pad}
        className={cn(
          "flex w-full cursor-pointer items-start gap-1.5 rounded-md py-1.5 pr-2 text-left transition-colors",
          selected === n.ws.id ? "bg-background shadow-xs ring-1 ring-border" : "hover:bg-accent",
        )}
      >
        <span className="flex h-5 items-center">{chevron}</span>
        <span className="flex h-5 items-center">
          <span className={cn("size-2 shrink-0 rounded-full", n.ws.online ? "bg-success" : "bg-muted-foreground/30")} />
        </span>
        <span className="grid min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-sm font-medium">{n.ws.name}</span>
            {n.ws.online > 0 && <span className="text-success shrink-0 text-xs">{n.ws.online} 在线</span>}
          </span>
          {depth === 0 ? (
            <Mono className="text-muted-foreground truncate text-xs">{n.path}</Mono>
          ) : (
            n.ws.name !== n.label && <Mono className="text-muted-foreground truncate text-xs">{n.label}</Mono>
          )}
        </span>
      </button>
    ) : (
      <div
        key={n.path}
        onClick={() => toggle(n.path)}
        title={n.path}
        style={pad}
        className="text-muted-foreground hover:bg-accent flex cursor-pointer items-center gap-1.5 rounded-md py-1.5 pr-2 select-none"
      >
        {chevron}
        <FolderClosed className="size-3.5 shrink-0" />
        <Mono className="min-w-0 truncate text-xs">{n.label}</Mono>
      </div>
    );
    return (
      <React.Fragment key={n.path}>
        {row}
        {hasKids && open && n.children.map((c) => render(c, depth + 1))}
      </React.Fragment>
    );
  };

  return <div className="space-y-0.5">{tree.map((n) => render(n, 0))}</div>;
}

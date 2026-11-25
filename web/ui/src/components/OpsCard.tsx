import type { Op } from "@/lib/api";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Mono } from "@/components/ui/mono";

export default function OpsTable({ ops, hideWorkspace }: { ops: Op[]; hideWorkspace?: boolean }) {
  if (ops.length === 0) return <p className="text-muted-foreground bg-card rounded-xl border px-4 py-6 text-center text-sm">还没有操作</p>;
  return (
    <div className="bg-card rounded-xl border px-2">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-24">时间</TableHead>
            <TableHead>身份</TableHead>
            {!hideWorkspace && <TableHead>工作区</TableHead>}
            <TableHead>操作</TableHead>
            <TableHead>对象</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {ops.slice(0, 60).map((e, i) => (
            <TableRow key={i}>
              <TableCell className="text-muted-foreground tabular-nums">{new Date(e.at).toLocaleTimeString()}</TableCell>
              <TableCell>{e.who}</TableCell>
              {!hideWorkspace && <TableCell>{e.ws}</TableCell>}
              <TableCell>
                <Badge variant={e.ok ? "secondary" : "destructive"}>{e.op}</Badge>
              </TableCell>
              <TableCell className="max-w-md truncate" title={`${e.target ?? ""}${e.ok ? "" : " — " + (e.note ?? "")}`}>
                <Mono>{e.target}</Mono>
                {!e.ok && <span className="text-destructive"> — {e.note}</span>}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

import { Activity } from "lucide-react";
import type { Op } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";

export default function OpsCard({ ops }: { ops: Op[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <Activity className="size-4" />
          最近操作
        </CardTitle>
      </CardHeader>
      <CardContent>
        {ops.length === 0 ? (
          <p className="text-muted-foreground text-sm">暂无</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-24">时间</TableHead>
                <TableHead>身份</TableHead>
                <TableHead>工作区</TableHead>
                <TableHead>操作</TableHead>
                <TableHead>对象</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ops.slice(0, 40).map((e, i) => (
                <TableRow key={i}>
                  <TableCell className="text-muted-foreground font-mono text-xs">{new Date(e.at).toLocaleTimeString()}</TableCell>
                  <TableCell>{e.who}</TableCell>
                  <TableCell>{e.ws}</TableCell>
                  <TableCell>
                    <Badge variant={e.ok ? "secondary" : "destructive"} className="font-mono">
                      {e.op}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-md truncate font-mono text-xs" title={`${e.target ?? ""}${e.ok ? "" : " — " + (e.note ?? "")}`}>
                    {e.target}
                    {!e.ok && <span className="text-destructive"> — {e.note}</span>}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

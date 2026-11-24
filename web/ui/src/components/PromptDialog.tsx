import { Copy } from "lucide-react";
import { toast } from "sonner";
import { copyText } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export default function PromptDialog({ value, onClose }: { value: { label: string; text: string } | null; onClose: () => void }) {
  return (
    <Dialog open={!!value} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>发给 {value?.label} 的连接提示</DialogTitle>
          <DialogDescription>原样粘贴给 AI。里面带着这个连接专属的令牌, 不要发到公开的地方。</DialogDescription>
        </DialogHeader>
        <pre className="bg-muted max-h-[50vh] overflow-auto rounded-lg p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
          {value?.text}
        </pre>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
          <Button
            onClick={async () => {
              await copyText(value?.text ?? "");
              toast.success("已复制");
              onClose();
            }}
          >
            <Copy />
            复制
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

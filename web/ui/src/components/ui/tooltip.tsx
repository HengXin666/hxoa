import * as React from "react";
import * as TP from "@radix-ui/react-tooltip";
import { cn } from "@/lib/utils";

export const TooltipProvider = TP.Provider;
export function Tip({ content, children }: { content: React.ReactNode; children: React.ReactNode }) {
  return (
    <TP.Root delayDuration={200}>
      <TP.Trigger asChild>{children}</TP.Trigger>
      <TP.Portal>
        <TP.Content sideOffset={4} className={cn("bg-primary text-primary-foreground z-50 max-w-xs rounded-md px-3 py-1.5 text-xs")}>
          {content}
        </TP.Content>
      </TP.Portal>
    </TP.Root>
  );
}

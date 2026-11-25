import * as React from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import { cn } from "@/lib/utils";

export const Tabs = ({ className, ...p }: React.ComponentProps<typeof TabsPrimitive.Root>) => (
  <TabsPrimitive.Root className={cn("flex flex-col gap-4", className)} {...p} />
);

export function TabsList({ className, ...p }: React.ComponentProps<typeof TabsPrimitive.List>) {
  return <TabsPrimitive.List className={cn("flex h-10 items-end gap-5 border-b", className)} {...p} />;
}

export function TabsTrigger({ className, ...p }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        "text-muted-foreground hover:text-foreground -mb-px inline-flex h-10 cursor-pointer items-center gap-1.5 border-b-2 border-transparent text-sm font-medium transition-colors outline-none",
        "data-[state=active]:border-foreground data-[state=active]:text-foreground [&_svg]:size-4",
        className,
      )}
      {...p}
    />
  );
}

export function TabsContent({ className, ...p }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content className={cn("outline-none", className)} {...p} />;
}

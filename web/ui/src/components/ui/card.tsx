import * as React from "react";
import { cn } from "@/lib/utils";

export function Card({ className, ...p }: React.ComponentProps<"div">) {
  return <div className={cn("bg-card text-card-foreground flex flex-col gap-5 rounded-xl border py-5 shadow-sm", className)} {...p} />;
}
export function CardHeader({ className, ...p }: React.ComponentProps<"div">) {
  return <div className={cn("grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-5 has-[[data-slot=card-action]]:grid-cols-[1fr_auto]", className)} {...p} />;
}
export function CardTitle({ className, ...p }: React.ComponentProps<"div">) {
  return <div className={cn("leading-none font-semibold flex items-center gap-2", className)} {...p} />;
}
export function CardDescription({ className, ...p }: React.ComponentProps<"div">) {
  return <div className={cn("text-muted-foreground text-sm", className)} {...p} />;
}
export function CardAction({ className, ...p }: React.ComponentProps<"div">) {
  return <div data-slot="card-action" className={cn("col-start-2 row-span-2 row-start-1 self-start justify-self-end", className)} {...p} />;
}
export function CardContent({ className, ...p }: React.ComponentProps<"div">) {
  return <div className={cn("px-5", className)} {...p} />;
}

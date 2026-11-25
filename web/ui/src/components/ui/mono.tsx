import * as React from "react";
import { cn } from "@/lib/utils";

// The one place monospace text (paths, branches, URLs, ids) gets its look,
// so every path in the panel renders with the same face and size.
export function Mono({ className, ...p }: React.ComponentProps<"span">) {
  return <span className={cn("font-mono text-[0.8125rem] leading-5 tracking-normal", className)} {...p} />;
}

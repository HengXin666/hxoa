import * as React from "react";
import * as AD from "@radix-ui/react-alert-dialog";
import { buttonVariantsFor } from "./button-variants";
import { cn } from "@/lib/utils";

export function AlertDialog(props: React.ComponentProps<typeof AD.Root>) {
  return <AD.Root {...props} />;
}
export function AlertDialogContent({ className, ...props }: React.ComponentProps<typeof AD.Content>) {
  return (
    <AD.Portal>
      <AD.Overlay className="fixed inset-0 z-50 bg-black/50" />
      <AD.Content
        className={cn(
          "bg-background fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-xl border p-6 shadow-lg sm:max-w-md",
          className,
        )}
        {...props}
      />
    </AD.Portal>
  );
}
export const AlertDialogTitle = ({ className, ...p }: React.ComponentProps<typeof AD.Title>) => (
  <AD.Title className={cn("text-lg font-semibold", className)} {...p} />
);
export const AlertDialogDescription = ({ className, ...p }: React.ComponentProps<typeof AD.Description>) => (
  <AD.Description className={cn("text-muted-foreground text-sm", className)} {...p} />
);
export const AlertDialogFooter = ({ className, ...p }: React.ComponentProps<"div">) => (
  <div className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)} {...p} />
);
export const AlertDialogCancel = ({ className, ...p }: React.ComponentProps<typeof AD.Cancel>) => (
  <AD.Cancel className={cn(buttonVariantsFor("outline"), className)} {...p} />
);
export const AlertDialogAction = ({ className, destructive, ...p }: React.ComponentProps<typeof AD.Action> & { destructive?: boolean }) => (
  <AD.Action className={cn(buttonVariantsFor(destructive ? "destructive" : "default"), className)} {...p} />
);

import * as React from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

interface Ask {
  title: string;
  description?: React.ReactNode;
  confirm?: string;
  destructive?: boolean;
}

const Ctx = React.createContext<(a: Ask) => Promise<boolean>>(async () => false);

export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [ask, setAsk] = React.useState<Ask | null>(null);
  const resolver = React.useRef<(v: boolean) => void>(undefined);
  const confirm = React.useCallback((a: Ask) => {
    setAsk(a);
    return new Promise<boolean>((res) => (resolver.current = res));
  }, []);
  const close = (v: boolean) => {
    resolver.current?.(v);
    setAsk(null);
  };
  return (
    <Ctx.Provider value={confirm}>
      {children}
      <AlertDialog open={!!ask} onOpenChange={(o) => !o && close(false)}>
        <AlertDialogContent>
          <AlertDialogTitle>{ask?.title}</AlertDialogTitle>
          {ask?.description && <AlertDialogDescription>{ask.description}</AlertDialogDescription>}
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => close(false)}>取消</AlertDialogCancel>
            <AlertDialogAction destructive={ask?.destructive} onClick={() => close(true)}>
              {ask?.confirm ?? "确定"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Ctx.Provider>
  );
}

export const useConfirm = () => React.useContext(Ctx);

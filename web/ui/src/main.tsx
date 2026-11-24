import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Toaster } from "sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ConfirmProvider } from "@/components/confirm";
import App from "./App";
import "./index.css";

if (window.matchMedia("(prefers-color-scheme: dark)").matches) document.documentElement.classList.add("dark");

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <TooltipProvider>
      <ConfirmProvider>
        <App />
      </ConfirmProvider>
      <Toaster position="top-center" richColors closeButton />
    </TooltipProvider>
  </StrictMode>,
);

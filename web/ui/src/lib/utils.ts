import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function ago(t?: string): string {
  if (!t || t.startsWith("0001")) return "从未连接";
  const s = (Date.now() - new Date(t).getTime()) / 1000;
  if (s < 60) return `${Math.max(0, Math.floor(s))} 秒前`;
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  return `${Math.floor(s / 86400)} 天前`;
}

export async function copyText(t: string) {
  try {
    await navigator.clipboard.writeText(t);
  } catch {
    const a = document.createElement("textarea");
    a.value = t;
    document.body.appendChild(a);
    a.select();
    document.execCommand("copy");
    a.remove();
  }
}

// Plain class strings for Radix parts that render their own <button>.
const base =
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium h-9 px-4 py-2 transition-all outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 cursor-pointer";
const variants: Record<string, string> = {
  default: "bg-primary text-primary-foreground shadow-xs hover:bg-primary/90",
  destructive: "bg-destructive text-white shadow-xs hover:bg-destructive/90",
  outline: "border bg-background shadow-xs hover:bg-accent",
};
export function buttonVariantsFor(v: keyof typeof variants | string) {
  return `${base} ${variants[v] ?? variants.default}`;
}

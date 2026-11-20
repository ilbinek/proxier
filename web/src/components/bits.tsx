import { useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { protoLabel } from "@/lib/format";
import type { EventLevel, Protocol, RuleState } from "@/lib/types";

export function StatusDot({ state, className }: { state: RuleState; className?: string }) {
  const color =
    state === "running" ? "bg-success" : state === "error" ? "bg-destructive" : "bg-muted-foreground/40";
  return (
    <span className={cn("relative inline-flex size-2.5 shrink-0", className)}>
      {state === "running" && (
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-40" />
      )}
      <span className={cn("relative inline-flex size-2.5 rounded-full", color)} />
    </span>
  );
}

export function StatusBadge({ state, error }: { state: RuleState; error?: string }) {
  const label = state === "running" ? "Running" : state === "error" ? "Error" : "Disabled";
  const badge = (
    <Badge
      variant="outline"
      className={cn(
        "gap-1.5 font-medium",
        state === "error" && "border-destructive/40 text-destructive",
        state === "disabled" && "text-muted-foreground",
      )}
    >
      <StatusDot state={state} />
      {label}
    </Badge>
  );
  if (!error) return badge;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span>{badge}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">{error}</TooltipContent>
    </Tooltip>
  );
}

const protoStyles: Record<string, string> = {
  tcp: "border-chart-2/40 text-chart-2",
  udp: "border-chart-4/40 text-chart-4",
  both: "border-chart-3/50 text-chart-3",
};

export function ProtoBadge({ proto, className }: { proto: Protocol | "tcp" | "udp"; className?: string }) {
  return (
    <Badge variant="outline" className={cn("font-mono text-[11px] tracking-tight", protoStyles[proto], className)}>
      {protoLabel(proto)}
    </Badge>
  );
}

export function LevelBadge({ level }: { level: EventLevel }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "h-5 w-14 shrink-0 self-start justify-center font-mono text-[10px] uppercase",
        level === "error" && "border-destructive/40 text-destructive",
        level === "warn" && "border-warning/50 text-warning",
        level === "info" && "text-muted-foreground",
      )}
    >
      {level}
    </Badge>
  );
}

export function CopyButton({ value, label, className }: { value: string; label?: string; className?: string }) {
  const [done, setDone] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setDone(true);
      setTimeout(() => setDone(false), 1500);
    } catch {
      toast.error("Clipboard not available (needs HTTPS or localhost)");
    }
  };
  return (
    <Button variant="ghost" size={label ? "sm" : "icon"} className={cn(!label && "size-7", className)} onClick={copy}>
      {done ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      {label}
    </Button>
  );
}

export function PageHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export function EmptyState({ icon, title, children }: { icon?: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed px-6 py-12 text-center">
      {icon && <div className="text-muted-foreground [&_svg]:size-8">{icon}</div>}
      <p className="font-medium">{title}</p>
      {children && <div className="max-w-md text-sm text-muted-foreground">{children}</div>}
    </div>
  );
}

export function FieldError({ msg }: { msg?: string }) {
  if (!msg) return null;
  return <p className="text-xs text-destructive">{msg}</p>;
}

import { PluginApiClient, PluginQueryClientProvider } from "@paca-ai/plugin-sdk-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronRight,
  Loader2,
  Plus,
  Send,
  Trash2,
  Webhook as WebhookIcon,
  X,
} from "lucide-react";
import { useMemo, useState } from "react";
import {
  type CreateWebhookInput,
  ErrorCode,
  getPluginErrorCode,
  listDeliveries,
  listWebhooks,
  testWebhook,
  updateWebhook,
  deleteWebhook,
  createWebhook,
  webhookDeliveriesKey,
  webhooksKey,
  WEBHOOK_EVENT_OPTIONS,
  type Webhook,
} from "./webhook-api";

// ── Utilities ──────────────────────────────────────────────────────────────────

function cn(...classes: (string | undefined | null | false)[]): string {
  return classes.filter(Boolean).join(" ");
}

const EVENT_LABELS = new Map(
  WEBHOOK_EVENT_OPTIONS.map((o) => [o.topic, o.label]),
);

const EVENT_GROUPS = Array.from(
  new Set(WEBHOOK_EVENT_OPTIONS.map((o) => o.group)),
).map((group) => ({
  group,
  options: WEBHOOK_EVENT_OPTIONS.filter((o) => o.group === group),
}));

// ── Primitives ─────────────────────────────────────────────────────────────────

function Btn({
  className,
  variant = "default",
  size = "default",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "outline" | "destructive" | "ghost";
  size?: "default" | "sm";
}) {
  const variants: Record<string, string> = {
    default: "bg-primary text-primary-foreground hover:bg-primary/90",
    outline: "border border-input bg-background hover:bg-accent",
    destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
    ghost: "hover:bg-accent",
  };
  const sizes: Record<string, string> = {
    default: "h-10 px-4 py-2",
    sm: "h-8 px-3 text-xs",
  };
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none",
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    />
  );
}

function Inp({ className, ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-muted", className)} />;
}

function EventBadge({ topic }: { topic: string }) {
  return (
    <span className="rounded-full bg-muted/60 px-2 py-0.5 text-xs font-medium text-muted-foreground">
      {EVENT_LABELS.get(topic) ?? topic}
    </span>
  );
}

// ── Event picker ───────────────────────────────────────────────────────────────

function EventPicker({
  selected,
  onChange,
}: {
  selected: string[];
  onChange: (topics: string[]) => void;
}) {
  const toggle = (topic: string) => {
    onChange(
      selected.includes(topic)
        ? selected.filter((t) => t !== topic)
        : [...selected, topic],
    );
  };

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
      {EVENT_GROUPS.map(({ group, options }) => (
        <div key={group} className="space-y-1.5">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground/70">
            {group}
          </p>
          {options.map((opt) => (
            <label
              key={opt.topic}
              className="flex items-center gap-2 text-sm cursor-pointer select-none"
            >
              <input
                type="checkbox"
                className="size-3.5 rounded border-input"
                checked={selected.includes(opt.topic)}
                onChange={() => toggle(opt.topic)}
              />
              {opt.label}
            </label>
          ))}
        </div>
      ))}
    </div>
  );
}

// ── Add webhook form ─────────────────────────────────────────────────────────

function AddWebhookForm({
  api,
  projectId,
  onDone,
}: {
  api: PluginApiClient;
  projectId: string;
  onDone: () => void;
}) {
  const queryClient = useQueryClient();
  const [url, setUrl] = useState("");
  const [secret, setSecret] = useState("");
  const [events, setEvents] = useState<string[]>(["task.created", "task.deleted"]);
  const [error, setError] = useState<string | null>(null);

  const createMutation = useMutation({
    mutationFn: () =>
      createWebhook(api, projectId, {
        url: url.trim(),
        secret: secret.trim() || undefined,
        events,
      } satisfies CreateWebhookInput),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: webhooksKey(projectId) });
      onDone();
    },
    onError: (err: unknown) => {
      const code = getPluginErrorCode(err);
      if (code === ErrorCode.WebhookInvalidUrl) {
        setError("Enter a valid https:// URL.");
      } else if (code === ErrorCode.WebhookInvalidEvents) {
        setError("Select at least one event.");
      } else {
        setError("Failed to create webhook. Please try again.");
      }
    },
  });

  return (
    <div className="space-y-4 rounded-lg border border-border/60 bg-muted/20 p-4">
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground">
          Webhook URL
        </label>
        <Inp
          type="url"
          placeholder="https://example.com/webhooks/paca"
          value={url}
          onChange={(e) => {
            setUrl(e.target.value);
            setError(null);
          }}
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground">
          Signing secret (optional)
        </label>
        <Inp
          type="text"
          placeholder="Used to sign the X-Paca-Signature header"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          autoComplete="off"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground">
          Events
        </label>
        <EventPicker selected={events} onChange={setEvents} />
      </div>
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
      <div className="flex justify-end gap-2">
        <Btn variant="outline" size="sm" onClick={onDone}>
          Cancel
        </Btn>
        <Btn
          size="sm"
          disabled={!url.trim() || events.length === 0 || createMutation.isPending}
          onClick={() => createMutation.mutate()}
        >
          {createMutation.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Plus className="size-3.5" />
          )}
          Add webhook
        </Btn>
      </div>
    </div>
  );
}

// ── Webhook item ───────────────────────────────────────────────────────────────

function WebhookItem({
  api,
  projectId,
  hook,
  canEdit,
}: {
  api: PluginApiClient;
  projectId: string;
  hook: Webhook;
  canEdit: boolean;
}) {
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);

  const { data: deliveries = [], isLoading: deliveriesLoading } = useQuery({
    queryKey: webhookDeliveriesKey(projectId, hook.id),
    queryFn: () => listDeliveries(api, projectId, hook.id),
    enabled: expanded,
  });

  const toggleMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      updateWebhook(api, projectId, hook.id, { enabled }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: webhooksKey(projectId) });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteWebhook(api, projectId, hook.id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: webhooksKey(projectId) });
    },
  });

  const testMutation = useMutation({
    mutationFn: () => testWebhook(api, projectId, hook.id),
    onSuccess: async (delivery) => {
      setTestResult(
        delivery.success
          ? `Delivered (HTTP ${delivery.status_code})`
          : `Failed: ${delivery.error || `HTTP ${delivery.status_code}`}`,
      );
      await queryClient.invalidateQueries({
        queryKey: webhookDeliveriesKey(projectId, hook.id),
      });
    },
    onError: () => setTestResult("Failed to send test event."),
  });

  return (
    <div className="rounded-lg border border-border/60 bg-background">
      <div className="flex items-start justify-between gap-3 p-3">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-start gap-2 text-left"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? (
            <ChevronDown className="size-4 shrink-0 mt-0.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="size-4 shrink-0 mt-0.5 text-muted-foreground" />
          )}
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{hook.url}</p>
            <div className="mt-1 flex flex-wrap gap-1">
              {(hook.events ?? []).map((ev) => (
                <EventBadge key={ev} topic={ev} />
              ))}
            </div>
          </div>
        </button>

        <div className="flex shrink-0 items-center gap-2">
          {canEdit && (
            <button
              type="button"
              onClick={() => toggleMutation.mutate(!hook.enabled)}
              disabled={toggleMutation.isPending}
              className={cn(
                "rounded-full px-2.5 py-1 text-xs font-semibold transition-colors",
                hook.enabled
                  ? "bg-emerald-500/15 text-emerald-600"
                  : "bg-muted text-muted-foreground",
              )}
            >
              {hook.enabled ? "Enabled" : "Disabled"}
            </button>
          )}
          {canEdit && (
            <Btn
              variant="ghost"
              size="sm"
              onClick={() => testMutation.mutate()}
              disabled={testMutation.isPending}
              title="Send test event"
            >
              {testMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Send className="size-3.5" />
              )}
            </Btn>
          )}
          {canEdit && (
            <Btn
              variant="ghost"
              size="sm"
              className="text-destructive/80 hover:text-destructive"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="size-3.5" />
            </Btn>
          )}
        </div>
      </div>

      {testResult && (
        <p className="px-3 pb-2 text-xs text-muted-foreground">{testResult}</p>
      )}

      {expanded && (
        <div className="border-t border-border/50 px-3 py-2">
          <p className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground/70">
            Recent deliveries
          </p>
          {deliveriesLoading ? (
            <Skeleton className="h-8" />
          ) : deliveries.length === 0 ? (
            <p className="text-xs text-muted-foreground">No deliveries yet.</p>
          ) : (
            <div className="space-y-1">
              {deliveries.map((d) => (
                <div
                  key={d.id}
                  className="flex items-center justify-between gap-2 text-xs"
                >
                  <span className="font-mono text-muted-foreground">
                    {EVENT_LABELS.get(d.event_type) ?? d.event_type}
                  </span>
                  <span
                    className={cn(
                      "flex items-center gap-1",
                      d.success ? "text-emerald-600" : "text-destructive",
                    )}
                  >
                    {d.success ? (
                      <Check className="size-3" />
                    ) : (
                      <X className="size-3" />
                    )}
                    {d.status_code ? `HTTP ${d.status_code}` : (d.error || "failed")}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {confirmDelete && (
        <div className="border-t border-border/50 bg-destructive/5 px-3 py-2.5 flex items-center justify-between gap-3">
          <p className="text-xs text-muted-foreground">
            Delete this webhook? This cannot be undone.
          </p>
          <div className="flex shrink-0 gap-2">
            <Btn
              variant="outline"
              size="sm"
              onClick={() => setConfirmDelete(false)}
              disabled={deleteMutation.isPending}
            >
              Cancel
            </Btn>
            <Btn
              variant="destructive"
              size="sm"
              onClick={() => deleteMutation.mutate()}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                "Delete"
              )}
            </Btn>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Main section ───────────────────────────────────────────────────────────────

function WebhookSettingsInner({
  api,
  projectId,
  canEdit,
}: {
  api: PluginApiClient;
  projectId: string;
  canEdit: boolean;
}) {
  const [adding, setAdding] = useState(false);

  const { data: hooks = [], isLoading } = useQuery({
    queryKey: webhooksKey(projectId),
    queryFn: () => listWebhooks(api, projectId),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">Webhooks</h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            Send an HTTP POST to a URL of your choice when task activity
            happens in this project.
          </p>
        </div>
        {canEdit && !adding && (
          <Btn size="sm" onClick={() => setAdding(true)}>
            <Plus className="size-3.5" />
            Add webhook
          </Btn>
        )}
      </div>

      {adding && (
        <AddWebhookForm
          api={api}
          projectId={projectId}
          onDone={() => setAdding(false)}
        />
      )}

      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-14 rounded-lg" />
          <Skeleton className="h-14 rounded-lg" />
        </div>
      ) : hooks.length > 0 ? (
        <div className="space-y-2">
          {hooks.map((hook) => (
            <WebhookItem
              key={hook.id}
              api={api}
              projectId={projectId}
              hook={hook}
              canEdit={canEdit}
            />
          ))}
        </div>
      ) : !adding ? (
        <div className="flex flex-col items-center gap-3 py-8 text-muted-foreground/60">
          <WebhookIcon className="size-8" />
          <p className="text-sm">No webhooks configured yet.</p>
          {canEdit && (
            <Btn variant="outline" size="sm" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" />
              Add your first webhook
            </Btn>
          )}
        </div>
      ) : null}

      {hooks.length > 0 && (
        <div className="flex items-start gap-2.5 rounded-lg bg-muted/40 border border-border/40 px-4 py-3">
          <AlertCircle className="size-4 text-muted-foreground/70 shrink-0 mt-0.5" />
          <p className="text-xs text-muted-foreground leading-relaxed">
            Each delivery is signed with{" "}
            <code className="font-mono">X-Paca-Signature</code> (HMAC-SHA256
            of the request body) when a secret is set.
          </p>
        </div>
      )}
    </div>
  );
}

// ── Public export ─────────────────────────────────────────────────────────────

interface WebhookSettingsTabProps {
  projectId: string;
  canEdit?: boolean;
}

export default function WebhookSettingsTab({
  projectId,
  canEdit = true,
}: WebhookSettingsTabProps) {
  const api = useMemo(
    () =>
      new PluginApiClient({
        baseUrl: `${window.location.origin}/api/v1`,
        projectId,
        fetch: (url, init) =>
          window.fetch(url, { ...init, credentials: "include" }),
      }),
    [projectId],
  );

  return (
    <PluginQueryClientProvider>
      <WebhookSettingsInner api={api} projectId={projectId} canEdit={canEdit} />
    </PluginQueryClientProvider>
  );
}

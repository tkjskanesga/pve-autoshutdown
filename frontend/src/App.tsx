import { useCallback, useEffect, useRef, useState } from "react";
import {
  Button,
  Chip,
  Description,
  FieldError,
  Input,
  Label,
  Modal,
  Spinner,
  TextField,
  useOverlayState,
  useTheme,
} from "@heroui/react";
import Sun from "@gravity-ui/icons/Sun";
import Moon from "@gravity-ui/icons/Moon";
import Display from "@gravity-ui/icons/Display";
import { api, type Guest, type JobEvent, type Preview } from "./api";

type PreviewState = "loading" | "ready" | "error";
type ThemeMode = "light" | "dark" | "system";
const THEME_ORDER: ThemeMode[] = ["light", "dark", "system"];
const THEME_WORD: Record<ThemeMode, string> = { light: "Light", dark: "Dark", system: "Auto" };

function eventColor(event: string): "default" | "accent" | "success" | "warning" | "danger" {
  switch (event) {
    case "stopped":
    case "done":
      return "success";
    case "force-stop":
    case "error":
      return "danger";
    case "still-running":
    case "cancelled":
      return "warning";
    case "shutdown-sent":
      return "accent";
    default:
      return "default";
  }
}

function ThemeToggle({
  theme,
  setTheme,
}: {
  theme: string;
  setTheme: (t: ThemeMode) => void;
}) {
  const mode = (THEME_ORDER.includes(theme as ThemeMode) ? theme : "system") as ThemeMode;
  const next = THEME_ORDER[(THEME_ORDER.indexOf(mode) + 1) % THEME_ORDER.length];
  return (
    <Button
      isIconOnly
      size="sm"
      variant="ghost"
      aria-label={`${THEME_WORD[mode]} theme, click to switch to ${THEME_WORD[next]}`}
      onPress={() => setTheme(next)}
    >
      {mode === "light" ? <Sun /> : mode === "dark" ? <Moon /> : <Display />}
    </Button>
  );
}

function LoginScreen({
  theme,
  setTheme,
  onDone,
}: {
  theme: string;
  setTheme: (t: ThemeMode) => void;
  onDone: () => void;
}) {
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setPending(true);
    setFailed(false);
    try {
      await api.login(password);
      onDone();
    } catch {
      setFailed(true);
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center bg-background p-4 text-foreground">
      <div className="absolute right-4 top-4">
        <ThemeToggle theme={theme} setTheme={setTheme} />
      </div>
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-3xl border border-border bg-surface p-6"
      >
        <h1 className="text-lg font-semibold">Automation Shutdown</h1>
        <p className="mt-1 text-sm text-muted">Sign in to access the shutdown switch.</p>
        <TextField
          className="mt-4 w-full"
          name="password"
          type="password"
          value={password}
          onChange={setPassword}
          isInvalid={failed}
          autoFocus
        >
          <Label>Password</Label>
          <Input className="w-full" placeholder="WebUI password" />
          {failed ? (
            <FieldError>Wrong password, try again.</FieldError>
          ) : (
            <Description>The password set in the WEB_PASSWORD env.</Description>
          )}
        </TextField>
        <Button className="mt-4" fullWidth type="submit" isPending={pending}>
          {pending ? "Checking..." : "Sign in"}
        </Button>
      </form>
    </div>
  );
}

function LogoutButton({ onLogout }: { onLogout: () => void }) {
  const state = useOverlayState();
  return (
    <>
      <Button size="sm" variant="ghost" onPress={state.open}>
        Sign out
      </Button>
      <Modal isOpen={state.isOpen} onOpenChange={state.setOpen}>
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog>
              <Modal.CloseTrigger />
              <Modal.Header>
                <Modal.Heading>Sign out of the console?</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <p className="text-sm">
                  Your session ends and you need the password to sign back in.
                </p>
              </Modal.Body>
              <Modal.Footer>
                <Button slot="close" variant="secondary">
                  Cancel
                </Button>
                <Button
                  variant="danger"
                  onPress={() => {
                    state.close();
                    onLogout();
                  }}
                >
                  Yes, sign out
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </>
  );
}

function TargetTable({ targets }: { targets: Guest[] }) {
  if (targets.length === 0) {
    return (
      <div className="rounded-2xl border border-border bg-surface px-6 py-12 text-center text-sm text-muted">
        No running vm right now...
      </div>
    );
  }
  return (
    <div className="overflow-x-auto rounded-2xl border border-border">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-separator text-xs text-muted">
            <th className="px-3 py-2 font-medium">VMID</th>
            <th className="px-3 py-2 font-medium">Name</th>
            <th className="px-3 py-2 font-medium">Node</th>
            <th className="px-3 py-2 font-medium">Type</th>
            <th className="px-3 py-2 font-medium">Tags</th>
          </tr>
        </thead>
        <tbody>
          {targets.map((g) => (
            <tr key={g.vmid} className="border-b border-separator last:border-0">
              <td className="px-3 py-2 font-mono">{g.vmid}</td>
              <td className="px-3 py-2">{g.name}</td>
              <td className="px-3 py-2">{g.node}</td>
              <td className="px-3 py-2">
                <Chip size="sm" variant="soft">
                  {g.type}
                </Chip>
              </td>
              <td className="px-3 py-2 text-muted">{g.tags.join(", ") || "-"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function LogPanel({ events, running }: { events: JobEvent[]; running: boolean }) {
  const boxRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = boxRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events.length]);
  return (
    <div
      ref={boxRef}
      className="log-scroll h-72 overflow-y-auto rounded-2xl border border-border bg-surface p-3 font-mono text-xs"
      aria-live="polite"
    >
      {events.length === 0 && (
        <p className="font-sans text-sm text-muted">
          {running ? "Waiting for the first event..." : "Run events appear here."}
        </p>
      )}
      {events.map((e, i) => (
        <div key={i} className="flex items-start gap-2 py-0.5">
          <span className="shrink-0 text-muted">
            {new Date(e.ts).toLocaleTimeString("en-GB", { hour12: false })}
          </span>
          <Chip size="sm" variant="soft" color={eventColor(e.event)}>
            {e.vmid ? `${e.vmid}` : e.event}
          </Chip>
          <span className="break-words">{e.message}</span>
        </div>
      ))}
      {running && (
        <div className="flex items-center gap-2 py-1">
          <Spinner size="sm" />
          <span className="font-sans text-sm text-muted">Running...</span>
        </div>
      )}
    </div>
  );
}

function ShutdownButton({
  total,
  disabled,
  starting,
  dryRun,
  onStart,
}: {
  total: number;
  disabled: boolean;
  starting: boolean;
  dryRun: boolean;
  onStart: () => void;
}) {
  const state = useOverlayState();
  const [password, setPassword] = useState("");
  const [checking, setChecking] = useState(false);
  const [wrong, setWrong] = useState(false);

  const open = () => {
    setPassword("");
    setWrong(false);
    state.open();
  };

  const confirm = async () => {
    setChecking(true);
    setWrong(false);
    try {
      await api.login(password);
      state.close();
      onStart();
    } catch {
      setWrong(true);
    } finally {
      setChecking(false);
    }
  };

  return (
    <>
      <Button size="lg" fullWidth variant="danger" isDisabled={disabled} isPending={starting} onPress={open}>
        {starting ? "Starting..." : "Shut down all VMs"}
      </Button>
      <Modal isOpen={state.isOpen} onOpenChange={state.setOpen}>
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog>
              <Modal.CloseTrigger />
              <Modal.Header>
                <Modal.Heading>Shut down all VMs?</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <p className="text-sm">
                  {total} running VMs will be powered off one by one. Guests that ignore the
                  shutdown signal past the timeout are force-stopped. This cannot be undone.
                  {dryRun ? " (DRY-RUN: no real action.)" : ""}
                </p>
                <TextField
                  className="mt-3 w-full"
                  name="confirm-password"
                  type="password"
                  value={password}
                  onChange={setPassword}
                  isInvalid={wrong}
                >
                  <Label>Type the password to approve</Label>
                  <Input className="w-full" placeholder="WebUI password" />
                  {wrong && <FieldError>Wrong password, shutdown cancelled.</FieldError>}
                </TextField>
              </Modal.Body>
              <Modal.Footer>
                <Button slot="close" variant="secondary">
                  Cancel
                </Button>
                <Button
                  variant="danger"
                  isDisabled={password === "" || checking}
                  isPending={checking}
                  onPress={confirm}
                >
                  {checking ? "Checking..." : `Yes, shut down ${total} VMs`}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </>
  );
}

function Console({
  theme,
  setTheme,
  preview,
  state,
  error,
  onRefresh,
  onLogout,
}: {
  theme: string;
  setTheme: (t: ThemeMode) => void;
  preview: Preview | null;
  state: PreviewState;
  error: string;
  onRefresh: () => void;
  onLogout: () => void;
}) {
  const [jobId, setJobId] = useState<string | null>(null);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [running, setRunning] = useState(false);
  const [starting, setStarting] = useState(false);

  useEffect(() => {
    if (!jobId) return;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws?jobId=${jobId}`);
    ws.onmessage = (msg) => {
      const e = JSON.parse(msg.data) as JobEvent;
      setEvents((prev) => [...prev, e]);
      if (e.event === "done" || e.event === "cancelled") setRunning(false);
    };
    ws.onerror = () => setRunning(false);
    return () => ws.close();
  }, [jobId]);

  const start = async () => {
    setStarting(true);
    try {
      const r = await api.shutdownAll();
      setEvents([]);
      setJobId(r.job_id);
      setRunning(true);
    } catch (e) {
      setEvents([
        {
          ts: new Date().toISOString(),
          job_id: "",
          event: "error",
          message: e instanceof Error ? e.message : "Could not start the job",
        },
      ]);
    } finally {
      setStarting(false);
    }
  };

  const cancel = useCallback(async () => {
    if (jobId) {
      try {
        await api.cancelJob(jobId);
      } catch {
        setRunning(false);
      }
    }
  }, [jobId]);

  const total = preview?.total_running ?? 0;
  const canStart = state === "ready" && total > 0 && !running && !starting;

  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="mx-auto flex max-w-3xl flex-col gap-4 p-4">
        <header className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-lg font-semibold">Automation Shutdown</h1>
          <div className="flex items-center gap-2">
            <ThemeToggle theme={theme} setTheme={setTheme} />
            <LogoutButton onLogout={onLogout} />
          </div>
        </header>

        {preview?.dry_run && (
          <p className="rounded-2xl border border-warning/40 bg-warning-soft px-4 py-2 text-sm text-warning-soft-foreground">
            DRY-RUN mode is on: runs are only logged, no VM is powered off.
          </p>
        )}

        <section className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm text-muted">
            {state === "loading" && "Loading the VM list..."}
            {state === "ready" && `${total} running VMs ready`}
            {state === "error" && "Could not load the VM list."}
            {preview && preview.tags_filter.length > 0 && state === "ready"
              ? ` (tags: ${preview.tags_filter.join(", ")})`
              : ""}
          </p>
          <Button size="sm" variant="outline" onPress={onRefresh} isDisabled={state === "loading"}>
            Reload
          </Button>
        </section>

        {state === "loading" && (
          <div className="flex items-center gap-2 rounded-2xl border border-border bg-surface p-4">
            <Spinner size="sm" />
            <span className="text-sm text-muted">Contacting Proxmox...</span>
          </div>
        )}
        {state === "error" && (
          <div className="rounded-2xl border border-danger/40 bg-danger-soft p-4">
            <p className="text-sm text-danger-soft-foreground">{error}</p>
            <Button className="mt-2" size="sm" variant="secondary" onPress={onRefresh}>
              Try again
            </Button>
          </div>
        )}
        {state === "ready" && <TargetTable targets={preview?.targets ?? []} />}

        <ShutdownButton
          total={total}
          disabled={!canStart}
          starting={starting || running}
          dryRun={preview?.dry_run ?? false}
          onStart={start}
        />

        {(events.length > 0 || running) && (
          <section className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold">Run log</h2>
              {running && (
                <Button size="sm" variant="outline" onPress={cancel}>
                  Cancel job
                </Button>
              )}
            </div>
            <LogPanel events={events} running={running} />
          </section>
        )}
      </div>
    </div>
  );
}

export default function App() {
  const { setTheme, theme } = useTheme("system");
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [state, setState] = useState<PreviewState>("loading");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    setState("loading");
    try {
      setPreview(await api.preview());
      setState("ready");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not load");
      setState("error");
    }
  }, []);

  useEffect(() => {
    api
      .me()
      .then(() => {
        setAuthed(true);
        refresh();
      })
      .catch(() => setAuthed(false));
  }, [refresh]);

  if (authed === null) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <Spinner />
      </div>
    );
  }
  if (!authed) {
    return <LoginScreen theme={theme} setTheme={setTheme} onDone={() => { setAuthed(true); refresh(); }} />;
  }
  return (
    <Console
      theme={theme}
      setTheme={setTheme}
      preview={preview}
      state={state}
      error={error}
      onRefresh={refresh}
      onLogout={() => {
        api.logout().finally(() => setAuthed(false));
      }}
    />
  );
}

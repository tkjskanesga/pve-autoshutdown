export interface Guest {
  vmid: number;
  name: string;
  node: string;
  type: string;
  status: string;
  tags: string[];
  template: boolean;
}

export interface Preview {
  total_running: number;
  targets: Guest[];
  tags_filter: string[];
  dry_run: boolean;
}

export interface JobEvent {
  ts: string;
  job_id: string;
  vmid?: number;
  node?: string;
  type?: string;
  event: string;
  message: string;
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (res.status === 401) throw new Error("unauthorized");
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data as { error?: string }).error ?? `HTTP ${res.status}`);
  return data as T;
}

export const api = {
  login: (password: string) =>
    req<{ authenticated: boolean }>("/api/login", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  logout: () => req("/api/logout", { method: "POST" }),
  me: () => req<{ authenticated: boolean; dry_run: boolean; tags_filter: string[] }>("/api/me"),
  preview: () => req<Preview>("/api/vms/preview"),
  shutdownAll: () => req<{ job_id: string; total: number; dry_run: boolean }>("/api/shutdown-all", { method: "POST" }),
  cancelJob: (id: string) => req(`/api/jobs/${id}`, { method: "DELETE" }),
};

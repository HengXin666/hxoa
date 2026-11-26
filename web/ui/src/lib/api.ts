// Relative URLs on purpose: the panel also runs under a sub-path
// (HX-Webx proxies it at /proxy/<name>/), so never start with "/".
export class ApiError extends Error {
  constructor(message: string, public status: number, public code?: string) {
    super(message);
  }
}

export async function api<T = any>(method: string, url: string, body?: unknown): Promise<T> {
  const r = await fetch(url.replace(/^\//, ""), {
    method,
    credentials: "same-origin",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  let j: any = null;
  try {
    j = await r.json();
  } catch {
    throw new ApiError(`服务器返回 ${r.status}`, r.status);
  }
  if (!j.ok) throw new ApiError(j.error || "请求失败", r.status, j.code);
  return j as T;
}

export interface AuthInfo {
  mode: "password" | "none";
  logged_in: boolean;
  user: string;
  username: string;
  has_account: boolean;
  seeded: boolean;
}

export interface WorkerStatus {
  exists: boolean;
  branch: string;
  ahead?: string;
  behind?: string;
  dirty?: string[];
  refs?: number[];
}

export interface Task {
  issues?: number[];
  note?: string;
  by?: "agent" | "user";
  at: string;
}

export interface TokenView {
  id: string;
  label: string;
  workspace: string;
  online: boolean;
  bye?: boolean;
  last_seen?: string;
  last_ip?: string;
  ops: number;
  worker?: WorkerStatus;
  task?: Task;
}

export interface WorkspaceView {
  id: string;
  name: string;
  path: string;
  workers: boolean;
  exec: boolean;
  git: boolean;
  branch?: string;
  repo?: string;
  online: number;
  tokens: TokenView[];
}

export interface Tunnel {
  mode: string;
  state: string;
  url?: string;
  error?: string;
  fixed?: boolean;
  hostname?: string;
  logs?: string[];
  hint?: string;
  network?: string;
}

export interface CheckStep {
  name: string;
  ok: boolean;
  detail: string;
  ms?: number;
}

export interface State {
  public_url: string;
  tunnel: Tunnel;
  workspaces: WorkspaceView[];
  server: string;
}

export interface Op {
  at: string;
  who: string;
  ws: string;
  op: string;
  target?: string;
  ok: boolean;
  note?: string;
}

export interface AdminToken {
  id: string;
  label: string;
  hint: string;
  created: string;
  last_seen?: string;
  last_ip?: string;
  ops: number;
}

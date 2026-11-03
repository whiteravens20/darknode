// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// The browser's side of the API. Every call goes to the same origin and
// carries the session cookie, which the server sets and the page cannot read.

export type Registration = 'open' | 'invite' | 'closed';

export interface InstanceConfig {
  registration: Registration;
  version: string;
}

export interface Line {
  text: string;
  kind?: '' | 'head' | 'dim' | 'ok' | 'error';
}

/**
 * Snapshot is the whole state, sent when the stream opens. Everything after
 * it arrives as a delta that names only what changed; `seq` numbers them all,
 * so a missing one is visible.
 */
export interface Snapshot {
  seq: number;
  account: { username: string };
  world_time: string;
  run: RunState | null;
}

/** RunDelta is the run's state after something changed it. */
export interface RunDelta {
  seq: number;
  run: RunState | null;
}

/** WorldDelta is the clock the whole instance shares. */
export interface WorldDelta {
  seq: number;
  world_time: string;
}

/** TraceDelta is the live trace, or null once none runs. */
export interface TraceDelta {
  seq: number;
  trace: Trace | null;
}

/** AlertDelta is something the game says of its own accord. */
export interface AlertDelta {
  seq: number;
  text: string;
  kind?: Line['kind'];
}

/** ConnectionDelta is where the player is connected, or null once nowhere. */
export interface ConnectionDelta {
  seq: number;
  connection: Connection | null;
}

/** ServerRef is a server as the player knows it: an address and a name. */
export interface ServerRef {
  ip: string;
  name: string;
}

/** Layer is one of a server's defences, and whether it has been opened. */
export interface Layer {
  name: string;
  open: boolean;
}

/** Tool is a program at work on the connection. */
export interface Tool {
  id: string;
  name: string;
  version: number;
  /** How far it has got, from 0 to 1. */
  progress: number;
  /** How long it has left at the share of the processor it is getting now. */
  left_seconds: number;
}

/**
 * Trace is how far the organisation has got in working out where the player
 * is. Without the TraceMeter the progress is rounded to quarters and the rest
 * is left out: the server decides what the page is allowed to know.
 */
export interface Trace {
  progress: number;
  exact: boolean;
  hop: number;
  hop_name: string;
  left_seconds: number;
}

/** Connection is the live connection, as the server reports it. */
export interface Connection {
  target: ServerRef;
  route: ServerRef[];
  state: 'CONNECTING' | 'CONNECTED';
  root: boolean;
  layers: Layer[];
  tools: Tool[];
}

export interface RunState {
  reputation: number;
  exposure_seconds: number;
  balance: { clean: number; dirty: number };
  gateway: {
    cpu_mhz: number;
    ram_mb: number;
    modem_mbps: number;
    storage_mb: number;
    nic_count: number;
    security: number;
  };
  software: { id: string; name: string; version: number }[];
  known_servers: number;
  route: ServerRef[];
  connection: Connection | null;
  trace: Trace | null;
}

/** ApiError carries the server's own message, which is written for the player. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body !== undefined) headers.set('Content-Type', 'application/json');

  let response: Response;
  try {
    response = await fetch(path, { ...init, headers, credentials: 'same-origin' });
  } catch {
    throw new ApiError(0, 'offline', 'The server cannot be reached.');
  }
  const text = await response.text();
  const body: unknown = text ? JSON.parse(text) : {};
  if (!response.ok) {
    const e = body as { error?: string; message?: string };
    throw new ApiError(response.status, e.error ?? 'error', e.message ?? 'Something went wrong.');
  }
  return body as T;
}

export const api = {
  config: () => request<InstanceConfig>('/api/config'),

  session: () => request<{ username: string }>('/api/session'),

  challenge: () =>
    request<{ id: string; bits: number; min_age_ms: number }>('/api/auth/challenge', {
      method: 'POST',
      body: '{}',
    }),

  register: (body: {
    username: string;
    password: string;
    challenge: string;
    solution: string;
    invite?: string;
  }) =>
    request<{ username: string; recovery_codes: string[] }>('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify({ ...body, website: '' }),
    }),

  login: (username: string, password: string) =>
    request<{ username: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  logout: () => request<{ ok: boolean }>('/api/auth/logout', { method: 'POST' }),

  recover: (username: string, code: string, password: string) =>
    request<{ username: string; codes_left: number }>('/api/auth/recover', {
      method: 'POST',
      body: JSON.stringify({ username, code, password }),
    }),

  /**
   * command sends one terminal line. The id is this command's own: if the
   * answer never arrives and the same id is sent again, the server replies
   * with what the first attempt did instead of running the line twice.
   */
  command: (line: string, clientCmdID: string) =>
    request<{ lines: Line[]; replayed?: boolean }>('/api/cmd', {
      method: 'POST',
      body: JSON.stringify({ line, client_cmd_id: clientCmdID }),
    }),
};

/**
 * solve finds the counter whose hash of `${id}:${counter}` starts with the
 * asked-for number of zero bits. It runs in the page because the work is the
 * point: a person waits a moment, a script that makes accounts in bulk pays
 * that moment every time.
 */
export async function solve(id: string, bits: number): Promise<string> {
  const encoder = new TextEncoder();
  for (let counter = 0; ; counter++) {
    const digest = new Uint8Array(
      await crypto.subtle.digest('SHA-256', encoder.encode(`${id}:${counter}`)),
    );
    if (leadingZeroBits(digest) >= bits) return String(counter);
  }
}

function leadingZeroBits(bytes: Uint8Array): number {
  let n = 0;
  for (const byte of bytes) {
    if (byte !== 0) return n + Math.clz32(byte) - 24;
    n += 8;
  }
  return n;
}

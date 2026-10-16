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

export interface Snapshot {
  account: { username: string };
  world_time: string;
  run: RunState | null;
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

  command: (line: string) =>
    request<{ lines: Line[] }>('/api/cmd', { method: 'POST', body: JSON.stringify({ line }) }),
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

// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { useCallback, useEffect, useState } from 'react';
import { ApiError, api, type Registration } from './api';
import { SignIn } from './SignIn';
import { Terminal } from './Terminal';
import { useStream } from './useStream';

export function App() {
  const [username, setUsername] = useState<string | null>(null);
  const [registration, setRegistration] = useState<Registration>('open');
  const [ready, setReady] = useState(false);

  // On load, ask the server who we are: the session lives in a cookie the
  // page cannot read, so only the server can answer.
  useEffect(() => {
    void (async () => {
      try {
        const config = await api.config();
        setRegistration(config.registration);
      } catch {
        // An instance that cannot be reached still shows the sign-in page.
      }
      try {
        const { username: name } = await api.session();
        setUsername(name);
      } catch (e) {
        if (!(e instanceof ApiError)) throw e;
      } finally {
        setReady(true);
      }
    })();
  }, []);

  const signedOut = useCallback(() => setUsername(null), []);
  const { snapshot, status } = useStream(username !== null, signedOut);

  if (!ready) return <div className="p-6 font-mono text-sm text-neutral-500">Loading…</div>;
  if (username === null) {
    return <SignIn registration={registration} onSignedIn={setUsername} />;
  }

  return (
    <div className="flex h-screen flex-col bg-neutral-950 text-neutral-200">
      <StatusBar username={username} snapshot={snapshot} status={status} />
      <Terminal onSignedOut={signedOut} />
    </div>
  );
}

function StatusBar({
  username,
  snapshot,
  status,
}: {
  username: string;
  snapshot: ReturnType<typeof useStream>['snapshot'];
  status: ReturnType<typeof useStream>['status'];
}) {
  const run = snapshot?.run;
  return (
    <header className="flex flex-wrap items-center gap-x-6 gap-y-1 border-b border-neutral-800 px-4 py-2 font-mono text-xs">
      <span className="font-semibold text-emerald-400">DarkNode</span>
      <span className="text-neutral-300">{username}</span>
      {snapshot && <span className="text-neutral-500">{formatWorldTime(snapshot.world_time)}</span>}
      {run ? (
        <>
          <span className="text-neutral-300">${run.balance.clean.toLocaleString('en')}</span>
          <span className="text-neutral-500">rep {run.reputation}</span>
          <span className="text-neutral-500">{run.known_servers} links</span>
        </>
      ) : (
        <span className="text-neutral-500">no run &mdash; type `run new`</span>
      )}
      <span className="ml-auto" title={streamTitle(status)}>
        <span className={status === 'live' ? 'text-emerald-500' : 'text-amber-500'}>&#9679;</span>
      </span>
    </header>
  );
}

function streamTitle(status: string): string {
  switch (status) {
    case 'live':
      return 'Connected to the server';
    case 'lost':
      return 'The connection dropped; it will be retried';
    case 'ended':
      return 'The session ended';
    default:
      return 'Connecting…';
  }
}

function formatWorldTime(iso: string): string {
  const t = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${t.getUTCFullYear()}-${pad(t.getUTCMonth() + 1)}-${pad(t.getUTCDate())} ${pad(t.getUTCHours())}:${pad(t.getUTCMinutes())}`;
}

// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { useCallback, useEffect, useState } from 'react';
import type { RunDelta, Snapshot, WorldDelta } from './api';

export type StreamStatus = 'connecting' | 'live' | 'ended' | 'lost';

/**
 * useStream keeps the player's state in step with the server. The stream
 * opens with a snapshot and carries a delta for everything that happens
 * afterwards, so the page never works out the state for itself.
 *
 * Every event is numbered. A number that does not follow the last one means a
 * delta was lost — a suspended tab, a connection that stopped reading — and
 * what the page holds is no longer the server's state. There is nothing to
 * ask for over a one-way stream, so the page opens the stream again and takes
 * a fresh snapshot.
 */
export function useStream(signedIn: boolean, onEnded: () => void) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [status, setStatus] = useState<StreamStatus>('connecting');
  // Bumped to open the stream again from the beginning.
  const [generation, setGeneration] = useState(0);
  const resync = useCallback(() => setGeneration((n) => n + 1), []);

  useEffect(() => {
    if (!signedIn) {
      setSnapshot(null);
      return;
    }
    const source = new EventSource('/api/stream');
    let ended = false;
    let seq = 0;

    /** inOrder reports whether a delta is the next one, and remembers it. */
    const inOrder = (next: number): boolean => {
      if (next !== seq + 1) return false;
      seq = next;
      return true;
    };

    const again = () => {
      ended = true;
      source.close();
      setStatus('connecting');
      resync();
    };

    const read = <T>(event: Event): T => JSON.parse((event as MessageEvent<string>).data) as T;

    source.addEventListener('snapshot', (event) => {
      const next = read<Snapshot>(event);
      seq = next.seq;
      setSnapshot(next);
      setStatus('live');
    });

    source.addEventListener('run', (event) => {
      const delta = read<RunDelta>(event);
      if (!inOrder(delta.seq)) return again();
      setSnapshot((s) => (s ? { ...s, run: delta.run } : s));
    });

    source.addEventListener('world', (event) => {
      const delta = read<WorldDelta>(event);
      if (!inOrder(delta.seq)) return again();
      setSnapshot((s) => (s ? { ...s, world_time: delta.world_time } : s));
    });

    // The server could not say what changed, so it asks for a clean start.
    source.addEventListener('resync', () => again());

    // The session was ended elsewhere: a sign-out in another window, or a
    // recovery. The browser must not reconnect; it has to sign in again.
    source.addEventListener('ended', () => {
      ended = true;
      source.close();
      setStatus('ended');
      onEnded();
    });

    source.onerror = () => {
      if (!ended) setStatus(source.readyState === EventSource.CLOSED ? 'lost' : 'connecting');
    };

    return () => {
      ended = true;
      source.close();
    };
  }, [signedIn, onEnded, resync, generation]);

  return { snapshot, status };
}

// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { useEffect, useState } from 'react';
import type { Snapshot } from './api';

export type StreamStatus = 'connecting' | 'live' | 'ended' | 'lost';

/**
 * useStream keeps the player's state in step with the server. The server
 * sends the whole state when the stream opens and again whenever it changes,
 * so the page never works out the state for itself.
 */
export function useStream(signedIn: boolean, onEnded: () => void) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [status, setStatus] = useState<StreamStatus>('connecting');

  useEffect(() => {
    if (!signedIn) {
      setSnapshot(null);
      return;
    }
    const source = new EventSource('/api/stream');
    let ended = false;

    source.addEventListener('state', (event) => {
      setSnapshot(JSON.parse((event as MessageEvent<string>).data) as Snapshot);
      setStatus('live');
    });

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
  }, [signedIn, onEnded]);

  return { snapshot, status };
}

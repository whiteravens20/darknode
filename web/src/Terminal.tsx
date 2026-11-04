// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { useEffect, useRef, useState } from 'react';
import { ApiError, api, type AlertDelta, type Line } from './api';

const WELCOME: Line[] = [
  { text: 'DarkNode', kind: 'head' },
  { text: '`help` lists the commands, `about` the licence.', kind: 'dim' },
  { text: 'New here? `run new`, then read the handbook: `connect 10.0.0.2`.', kind: 'dim' },
  { text: '' },
];

const kindClass: Record<string, string> = {
  head: 'text-emerald-300 font-semibold',
  dim: 'text-neutral-500',
  ok: 'text-emerald-400',
  error: 'text-rose-400',
};

/** Commands the terminal answers itself, without asking the server. */
const CLIENT_COMMANDS = new Set(['clear', 'history', 'logout']);

export function Terminal({
  onSignedOut,
  alerts,
}: {
  onSignedOut: () => void;
  alerts: AlertDelta[];
}) {
  const [lines, setLines] = useState<Line[]>(WELCOME);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState<string[]>([]);
  const [historyAt, setHistoryAt] = useState<number | null>(null);
  const bottom = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: 'end' });
  }, [lines]);

  // The game says things without being asked — an administrator closing a
  // layer, a connection cut. Each one is printed once, in the order it came.
  const printed = useRef(0);
  useEffect(() => {
    const fresh = alerts.filter((a) => a.seq > printed.current);
    if (fresh.length === 0) return;
    printed.current = fresh[fresh.length - 1]?.seq ?? printed.current;
    setLines((all) => [
      ...all,
      ...fresh.map((a): Line => (a.kind ? { text: a.text, kind: a.kind } : { text: a.text })),
      { text: '' },
    ]);
  }, [alerts]);

  const print = (more: Line[]) => setLines((all) => [...all, ...more]);

  async function submit(raw: string) {
    const line = raw.trim();
    setInput('');
    setHistoryAt(null);
    if (line === '') return;
    setHistory((h) => (h[h.length - 1] === line ? h : [...h, line]));
    print([{ text: `> ${line}` }]);

    const word = line.split(/\s+/)[0]?.toLowerCase() ?? '';
    if (CLIENT_COMMANDS.has(word)) {
      await clientCommand(word);
      return;
    }

    setBusy(true);
    try {
      // Each command carries an id of its own, so a request the browser has
      // to repeat cannot run the line twice.
      const { lines: out } = await api.command(line, crypto.randomUUID());
      print([...out, { text: '' }]);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        onSignedOut();
        return;
      }
      print([
        { text: e instanceof Error ? e.message : 'Something went wrong.', kind: 'error' },
        { text: '' },
      ]);
    } finally {
      setBusy(false);
      field.current?.focus();
    }
  }

  async function clientCommand(word: string) {
    switch (word) {
      case 'clear':
        setLines([]);
        return;
      case 'history':
        print([
          ...history.map((h, i) => ({ text: `  ${String(i + 1).padStart(3)}  ${h}` })),
          { text: '' },
        ]);
        return;
      case 'logout':
        try {
          await api.logout();
        } finally {
          onSignedOut();
        }
    }
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      event.preventDefault();
      if (history.length === 0) return;
      const next =
        event.key === 'ArrowUp'
          ? Math.max(0, (historyAt ?? history.length) - 1)
          : Math.min(history.length, (historyAt ?? history.length) + 1);
      setHistoryAt(next);
      setInput(next === history.length ? '' : (history[next] ?? ''));
    }
  }

  return (
    <div
      className="flex min-h-0 flex-1 cursor-text flex-col bg-neutral-950 p-4 font-mono text-sm"
      onClick={() => field.current?.focus()}
    >
      <div className="min-h-0 flex-1 overflow-y-auto">
        {lines.map((line, i) => (
          <div
            key={i}
            className={`whitespace-pre-wrap break-words ${kindClass[line.kind ?? ''] ?? 'text-neutral-200'}`}
          >
            {line.text || ' '}
          </div>
        ))}
        <div ref={bottom} />
      </div>
      <form
        className="mt-2 flex items-center gap-2 border-t border-neutral-800 pt-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!busy) void submit(input);
        }}
      >
        <span aria-hidden className="text-emerald-400">
          &gt;
        </span>
        <input
          ref={field}
          autoFocus
          aria-label="Command"
          className="flex-1 bg-transparent text-neutral-100 caret-emerald-400 outline-none disabled:opacity-50"
          value={input}
          disabled={busy}
          spellCheck={false}
          autoComplete="off"
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={onKeyDown}
        />
      </form>
    </div>
  );
}

// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { useEffect, useRef, useState } from 'react';
import { ApiError, api, type Line } from './api';

const WELCOME: Line[] = [
  { text: 'DarkNode', kind: 'head' },
  { text: 'Type `help` for the commands, `about` for the licence.', kind: 'dim' },
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

export function Terminal({ onSignedOut }: { onSignedOut: () => void }) {
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
      const { lines: out } = await api.command(line);
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

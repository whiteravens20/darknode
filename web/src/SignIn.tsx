// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { useEffect, useRef, useState } from 'react';
import { ApiError, api, solve, type Registration } from './api';

type Mode = 'login' | 'register' | 'recover';

/** Puzzle is a challenge from the server, with the moment it arrived. */
interface Puzzle {
  id: string;
  bits: number;
  min_age_ms: number;
  received: number;
}

export function SignIn({
  registration,
  onSignedIn,
}: {
  registration: Registration;
  onSignedIn: (username: string) => void;
}) {
  const [mode, setMode] = useState<Mode>('login');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [invite, setInvite] = useState('');
  const [code, setCode] = useState('');
  const [trap, setTrap] = useState(''); // hidden; only a bot fills it in
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [codes, setCodes] = useState<string[] | null>(null);

  // The puzzle is asked for as soon as the registration form is opened, not
  // when it is sent. The server will not accept a solution that comes back
  // within a couple of seconds of handing the puzzle out, which is the point:
  // a person spends that time filling the form in. Asking at send time would
  // instead measure how fast the computer solves it, and turn away the quick
  // ones.
  const challenge = useRef<Puzzle | null>(null);
  useEffect(() => {
    if (mode !== 'register' || registration === 'closed') return;
    let dropped = false;
    void api
      .challenge()
      .then((c) => {
        if (!dropped) challenge.current = { ...c, received: Date.now() };
      })
      .catch(() => {
        // The form still works: sending it asks for a puzzle then.
      });
    return () => {
      dropped = true;
    };
  }, [mode, registration]);

  /**
   * puzzle returns a solved puzzle, asking for a fresh one when there is
   * none, and waiting out the minimum age the server asks for.
   */
  async function puzzle(): Promise<{ challenge: string; solution: string }> {
    const c = challenge.current ?? { ...(await api.challenge()), received: Date.now() };
    challenge.current = null; // each puzzle is accepted once
    const solution = await solve(c.id, c.bits);
    const left = c.received + c.min_age_ms - Date.now();
    if (left > 0) await new Promise((done) => setTimeout(done, left));
    return { challenge: c.id, solution };
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    try {
      if (mode === 'login') {
        setBusy('Signing in…');
        const { username: name } = await api.login(username, password);
        onSignedIn(name);
        return;
      }
      if (mode === 'recover') {
        setBusy('Checking…');
        const { username: name, codes_left } = await api.recover(username, code, password);
        setError('');
        alertCodesLeft(codes_left);
        onSignedIn(name);
        return;
      }
      setBusy('Creating the account…');
      const result = await register();
      setCodes(result.recovery_codes);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Something went wrong.');
    } finally {
      setBusy(null);
    }
  }

  /**
   * register sends the form. A puzzle that expired while the form was open is
   * replaced and the form sent again, once, rather than shown as an error the
   * player can do nothing about.
   */
  async function register() {
    const send = async () =>
      api.register({
        username,
        password,
        ...(await puzzle()),
        ...(invite ? { invite } : {}),
      });
    try {
      return await send();
    } catch (e) {
      if (e instanceof ApiError && e.code === 'pow') {
        challenge.current = null;
        return await send();
      }
      throw e;
    }
  }

  if (codes) {
    return <RecoveryCodes codes={codes} onDone={() => onSignedIn(username)} />;
  }

  const closed = mode === 'register' && registration === 'closed';

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col justify-center p-6">
      <h1 className="font-mono text-3xl font-semibold text-emerald-400">DarkNode</h1>
      <p className="mt-1 mb-6 text-sm text-neutral-400">
        A self-hosted hacking simulator. Break in, cover your tracks, outrun the trace.
      </p>

      <nav className="mb-4 flex gap-2 text-sm">
        {(['login', 'register', 'recover'] as Mode[]).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => {
              setMode(m);
              setError('');
            }}
            className={`rounded px-3 py-1 ${
              mode === m
                ? 'bg-emerald-500/15 text-emerald-300'
                : 'text-neutral-400 hover:text-neutral-200'
            }`}
          >
            {m === 'login' ? 'Sign in' : m === 'register' ? 'New agent' : 'Lost password'}
          </button>
        ))}
      </nav>

      {closed ? (
        <p className="rounded border border-neutral-800 bg-neutral-900 p-4 text-sm text-neutral-300">
          This instance is not taking new agents. Ask whoever runs it for an account.
        </p>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <Field
            label="Agent name"
            value={username}
            onChange={setUsername}
            autoComplete="username"
          />
          {mode === 'recover' && (
            <Field
              label="Recovery code"
              value={code}
              onChange={setCode}
              placeholder="XXXX-XXXX-XXXX"
            />
          )}
          <Field
            label={mode === 'login' ? 'Password' : 'New password'}
            value={password}
            onChange={setPassword}
            type="password"
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />
          {mode === 'register' && registration === 'invite' && (
            <Field
              label="Invite code"
              value={invite}
              onChange={setInvite}
              placeholder="XXXX-XXXX-XXXX"
            />
          )}
          {mode === 'register' && (
            <input
              aria-hidden
              tabIndex={-1}
              autoComplete="off"
              name="website"
              value={trap}
              onChange={(e) => setTrap(e.target.value)}
              className="absolute left-[-9999px] h-0 w-0"
            />
          )}

          {error && <p className="text-sm text-rose-400">{error}</p>}

          <button
            type="submit"
            disabled={busy !== null}
            className="mt-2 rounded bg-emerald-500/15 px-4 py-2 text-emerald-300 hover:bg-emerald-500/25 disabled:opacity-50"
          >
            {busy ??
              (mode === 'login'
                ? 'Sign in'
                : mode === 'recover'
                  ? 'Set a new password'
                  : 'Create the account')}
          </button>
          {mode === 'register' && (
            <p className="text-xs text-neutral-500">
              No e-mail is asked for. You get recovery codes instead: keep them, they are the only
              way back in.
            </p>
          )}
        </form>
      )}
    </div>
  );
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [kept, setKept] = useState(false);
  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col justify-center p-6">
      <h2 className="font-mono text-xl text-emerald-400">Your recovery codes</h2>
      <p className="mt-2 text-sm text-neutral-300">
        Each code works once and sets a new password. They are shown now and never again, so write
        them down or save them somewhere safe.
      </p>
      <ul className="my-4 grid grid-cols-2 gap-2 rounded border border-neutral-800 bg-neutral-900 p-4 font-mono text-sm text-neutral-100">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <button
        type="button"
        onClick={() => navigator.clipboard?.writeText(codes.join('\n'))}
        className="mb-3 rounded border border-neutral-700 px-3 py-1.5 text-sm text-neutral-300 hover:bg-neutral-800"
      >
        Copy them
      </button>
      <label className="mb-3 flex items-center gap-2 text-sm text-neutral-300">
        <input type="checkbox" checked={kept} onChange={(e) => setKept(e.target.checked)} />I have
        saved these codes.
      </label>
      <button
        type="button"
        disabled={!kept}
        onClick={onDone}
        className="rounded bg-emerald-500/15 px-4 py-2 text-emerald-300 hover:bg-emerald-500/25 disabled:opacity-40"
      >
        Start playing
      </button>
    </div>
  );
}

function alertCodesLeft(left: number) {
  if (left === 0) {
    window.alert('That was your last recovery code. Ask the operator for a new set.');
  }
}

function Field({
  label,
  value,
  onChange,
  type = 'text',
  autoComplete,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  autoComplete?: string;
  placeholder?: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-sm text-neutral-300">
      {label}
      <input
        required
        type={type}
        value={value}
        placeholder={placeholder}
        autoComplete={autoComplete}
        onChange={(e) => onChange(e.target.value)}
        className="rounded border border-neutral-700 bg-neutral-900 px-3 py-2 font-mono text-neutral-100 outline-none focus:border-emerald-600"
      />
    </label>
  );
}

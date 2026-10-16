# Security — DarkNode

## Reporting a vulnerability

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/whiteravens20/darknode/security/advisories/new).
Please do not open a public issue, discussion or pull request for a security
problem.

Say which version or commit you tested, how to reproduce it, and what you
think it leads to. You will get a first answer within a week. A confirmed
problem is fixed in a new release, and the advisory credits you unless you
would rather it did not.

## Supported versions

The project is in early development and has no release yet. Until the first
one, only the newest commit on `dev` is fixed.

## What counts

The game is about breaking into fictional computers, so please be clear about
which kind of problem you are reporting.

**In scope** — anything that breaks the real software:

- Reaching another player's account, session or run.
- Making the server do something as another player, or without a session.
- Reading or changing data the server should not have let you touch.
- Getting out of the container, running a command on the host, or reaching the
  database directly.
- Breaking the sign-up protections well enough to make accounts in bulk.
- Anything that leaks a password, a session token, a recovery code or an
  invite code.

**Not a vulnerability** — winning at the game:

- Doing well inside the rules, or finding an advantage the rules allow. That
  is an issue about balance, and a normal issue is the place for it.
- Reading the world's data files. They ship with the server and are public.
- Automating play on your own instance.

## What the project does about it

These are the controls the code keeps, so you know what to test against.

- **The server decides everything.** The browser shows state and sends
  commands. Nothing a player's page claims about the state is believed.
- **Passwords** are hashed with argon2id. A sign-in for a name that does not
  exist spends the same time as a wrong password, so the answer does not say
  which accounts exist.
- **Sessions** live in a table on the server, named by a cookie the page
  cannot read. The cookie is HttpOnly, SameSite=Lax, and over HTTPS it carries
  the `__Host-` prefix. A session ends after two weeks unused, and after sixty
  days whatever happens. Recovering an account ends every other session.
- **Recovery and invite codes** are stored as HMACs keyed with a secret that
  only the server holds. A copy of the database does not reveal a code.
- **Sign-ups** are behind a proof-of-work the browser computes, limits per
  network, and a trap field. No third-party service is involved.
- **Sign-ins** slow down after repeated failures rather than locking an
  account, so nobody can lock another player out.
- **Addresses** are used only for short-lived rate limits, in memory, and are
  never written down. `X-Forwarded-For` is believed only from a proxy the
  operator named.
- **The page** fetches nothing from another site; the content security policy
  allows only its own origin.
- **The image** holds one static binary, the world's data and the built page:
  no shell, no package manager, not root, and a read-only filesystem. CI
  checks all of this on every change.

## For whoever runs an instance

- Put it behind a reverse proxy that terminates TLS, and leave
  `DARKNODE_COOKIE_SECURE` at `true`.
- Keep `secrets/` to yourself: the directory is what protects the files on the
  host. Back up `server_secret` with the database, because every recovery and
  invite code is tied to it.
- Name your proxy in `DARKNODE_TRUSTED_PROXIES`, and nothing else. Trusting a
  network you do not control lets a visitor claim any address they like.
- The database publishes no port of its own in the supplied compose file.
  Leave it that way.

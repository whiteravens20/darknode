# Contributing to DarkNode

Thank you for looking. Please read this before you open a pull request.

## Before you start

- Look through the [open issues](../../issues) and [pull requests](../../pulls)
  so two people do not write the same thing.
- For anything larger than a fix, open an issue first and agree on the shape
  of it. A big pull request nobody expected is hard to take.
- By contributing you accept the [licence](LICENSE) and the
  [Code of Conduct](CODE_OF_CONDUCT.md).

## What this project takes

**Welcome:** fixes, tests, clearer wording in the game and in the docs,
accessibility, and content for the world (organisations, servers, files) that
fits the setting.

**Not taken:**

- Anything that moves a decision from the server into the browser. The server
  decides every outcome; the page shows state and sends commands. A change
  that lets the page decide is a change that lets a player decide.
- Anything that collects data about the people who play. The game asks for no
  e-mail address and keeps no addresses beyond short-lived rate limits.
- Anything the page fetches from another site: no fonts, no scripts, no
  analytics, no anti-bot service. An instance has to work with no outbound
  traffic at all.
- A weaker security control without a reason that stands up in review.

## Getting set up

You need Go (the version in `go.mod`), Node 24 for the client, and a
PostgreSQL to develop against. The quickest database is a container:

```bash
docker run -d --name darknode-dev-db -e POSTGRES_PASSWORD=postgres \
  -p 127.0.0.1:5432:5432 postgres:18.6-alpine
```

Then run the server:

```bash
export DARKNODE_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
export DARKNODE_SECRET="$(openssl rand -base64 48)"
export DARKNODE_COOKIE_SECURE=false      # the dev client is plain HTTP
go run ./cmd/darknode serve
```

And the client, in another terminal:

```bash
cd web
npm ci --ignore-scripts
npm run dev
```

The page is then on <http://localhost:5173> and talks to the server through
Vite's proxy, so the browser sees one origin and the session cookie behaves as
it does in production.

## Where things are

| Path | What is in it |
|---|---|
| `cmd/darknode/` | The command: serving, migrating, invites, recovery codes |
| `internal/config/` | Settings, read from the environment |
| `internal/store/` | PostgreSQL: the schema, its migrations and the queries |
| `internal/auth/` | Password hashing, the sign-up puzzle, rate limits, codes |
| `internal/game/` | The game itself: the clock, runs, the terminal's commands |
| `internal/server/` | HTTP: the API, sessions, the state stream |
| `internal/world/` | The world catalogue and the loader that checks it |
| `data/world/` | The world itself, as data files |
| `web/` | The browser client |

## How to write it

- One logical change per pull request, and no unrelated edits carried along.
- No dead code and no commented-out blocks.
- Go code is `gofmt`-formatted and passes `go vet ./...` and staticcheck.
- Client code is formatted with Prettier and type-checks with no errors.
- A new dependency needs its reason in the pull request. Both lockfiles are
  committed.

### Tests

A test says what the code has to do, and it is there to catch the code where
it does not. Write it from the behaviour you want, not from what the code
happens to do today, and never relax one to make a red run green.

```bash
export DARKNODE_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
go test ./...
cd web && npm run typecheck && npm run build
```

The tests that need a database make one of their own and drop it afterwards.
Without `DARKNODE_TEST_DATABASE_URL` they skip, which is fine on a laptop; CI
sets `DARKNODE_TEST_REQUIRE_DB`, so there a missing database fails the run.

### Commits

[Conventional Commits](https://www.conventionalcommits.org/), one topic per
commit, a single line:

```
feat: let a player see the servers they know
fix: stop the trace when the run ends
```

Commits must be signed.

## Writing securely

- Treat everything from the browser as untrusted, and check it on the server.
  The browser's copy of the state is for showing, never for deciding.
- Never log a password, a session token, a recovery code or an invite code.
  The codes are stored as HMACs; keep it that way.
- Never put a secret in the image, in a commit, or in a test fixture. If you
  commit one by accident, treat it as known to everyone, replace it, and
  report it privately.
- A change to the schema is a new migration file. Never edit one that has been
  applied: the server refuses to start against a database whose history does
  not match the build.
- The stage of the Dockerfile that ships carries no shell and no package
  manager. CI checks this, and the check is not to be relaxed.

### AI-assisted code

Most of this project is written with AI coding tools, as the
[README](README.md#how-the-code-is-written-and-checked) says, and your
contribution may be too. Do not send output you cannot explain and defend in
review: read it, test it, and take responsibility for it.

## Sending the change

1. Branch from `dev`.
2. Make the change, with tests.
3. Open a pull request into `dev` and fill in the template.
4. CI has to pass before it is merged.

## Reporting a security problem

Do not open a public issue for one. See [SECURITY.md](SECURITY.md).

# DarkNode

> A self-hosted hacking simulator for the browser: break in, cover your tracks, outrun the trace.

[![License](https://img.shields.io/github/license/whiteravens20/darknode)](LICENSE)
[![Test](https://github.com/whiteravens20/darknode/actions/workflows/test.yml/badge.svg)](https://github.com/whiteravens20/darknode/actions/workflows/test.yml)
[![CodeQL](https://github.com/whiteravens20/darknode/actions/workflows/codeql.yml/badge.svg)](https://github.com/whiteravens20/darknode/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/whiteravens20/darknode/badge)](https://scorecard.dev/viewer/?uri=github.com/whiteravens20/darknode)

DarkNode is a game in the line of *Uplink* and *Hacker Evolution*. You are an
agent with one computer and a list of addresses. You take contracts, break
into the machines of invented organisations, take or destroy what you were
paid to, clear your traces, and get out before anyone works out where you are.
Everything you break into is fiction, and it lives on the server you or
somebody else runs.

> **Early development.** The world, the contracts and the trace are being
> built. What works today is the account, the gateway, a run, and the terminal
> that shows them. There is no release yet.

## What is there

- **A terminal as the whole interface.** Every action has a command; the
  panels that come later will be shortcuts for the same ones.
- **A world shared by everyone on the instance.** The organisations, their
  machines and their files are data files in this repository, the same for
  every player; what you do to the world is yours alone.
- **A server that decides everything.** Timers, outcomes and dice rolls run on
  the server. Opening the browser's tools shows you the page, not the game.
- **Accounts without e-mail.** You get recovery codes instead. Sign-ups are
  held back by a puzzle your browser works out, not by a service somewhere
  else.
- **Nothing fetched from anywhere else.** No fonts, no scripts, no analytics.
  An instance works with no outbound traffic at all.

## Run your own

You need Docker with the Compose plugin.

```bash
git clone https://github.com/whiteravens20/darknode
cd darknode

# The two secrets live in files, so they stay out of the environment.
mkdir -p secrets
openssl rand -base64 32 > secrets/db_password
openssl rand -base64 48 > secrets/server_secret
chmod 700 secrets && chmod 444 secrets/db_password secrets/server_secret

cp .env.example .env     # read it: the port, who may register, HTTPS
docker compose up -d
```

The game is then on <http://localhost:8080>. It is published on the loopback
address: put a reverse proxy with TLS in front before anyone else reaches it,
and leave `DARKNODE_COOKIE_SECURE` at `true` when you do.

To keep it to people you invite, set `DARKNODE_REGISTRATION=invite` and make
codes:

```bash
docker compose exec game /darknode invite -n 5
```

The settings are listed in [.env.example](.env.example), and what to watch out
for when hosting is in [SECURITY.md](SECURITY.md#for-whoever-runs-an-instance).

## Play

Make an agent, **save the recovery codes it gives you** — they are shown once
and they are the only way back into the account — and you are at a terminal.

```
help              what you can type
run new           start a run: a gateway, a little money, two programs
status            what you have
servers           the addresses you know
about             licence and source
```

The world has a practice machine with nothing on it that matters, and a
handbook server to read. Start there.

## Build it yourself

You need Go (the version in `go.mod`), Node 24 and a PostgreSQL. The steps are
in [CONTRIBUTING.md](CONTRIBUTING.md#getting-set-up).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md). Content for the world is as welcome as
code.

## Security

See [SECURITY.md](SECURITY.md) for how to report a problem privately, what
counts as one, and the controls the project keeps.

## How the code is written and checked

DarkNode is built using AI coding tools. The tools write most of the code,
tests and documentation; the maintainer decides what gets built and is
responsible for everything that is merged. The project relies on checks that
anyone can inspect, and before the first release is published a group of human
testers will try the release candidate at the maintainer's request. The
project is in early development, has no release yet, and has had no
independent security review.

**What a change goes through**

Every push and pull request on `main` and `dev` runs
[test.yml](.github/workflows/test.yml): the server is checked for formatting
and tidy module files, vetted, built, and its tests run with the race detector
against a real PostgreSQL, so the tests that are about the schema really meet
one; the client is checked for formatting, type-checked and built; the image
is built and then checked to hold no shell and no package manager and not to
run as root. [security.yml](.github/workflows/security.yml) runs govulncheck
and staticcheck on the server, `npm audit` and a signature check on the
client's packages, Trivy over the tree and over the image, and two checks that
every action is pinned to the commit of the version named beside it.
[codeql.yml](.github/workflows/codeql.yml) analyses the Go and the TypeScript.
All of it runs again weekly, so a vulnerability published after a change still
surfaces. Commits are signed.

**What the maintainer decided and read**

The design decisions were the maintainer's: that the server decides every
outcome rather than the browser, that the world is an authored catalogue
shared by everyone on an instance, that accounts need no e-mail address and
are recovered with one-time codes, that sign-ups are held back by a
proof-of-work computed in the browser rather than by a third-party service,
and that the image that ships carries no shell and no package manager. The
maintainer reads every change to the code that decides who gets in and as
whom — `internal/auth/` and the session, registration and sign-in handlers in
`internal/server/` — line by line before it is merged.

If something looks wrong, open an issue. For a vulnerability, follow
[SECURITY.md](SECURITY.md).

## Trademarks and other games

*Uplink* is a trademark of Introversion Software. *Hacker Evolution* is a
trademark of exosyphen studios. DarkNode is not affiliated with, endorsed by
or connected to either of them, nor to anyone else named on this page.

Those games are mentioned to say what kind of game this is and nothing more.
DarkNode shares no code, assets, text or data with them: it was written from
nothing, and everything in it is the work of this project's contributors
under the licence below. Every organisation, person, machine and address in
the game is invented; any resemblance to a real one is accidental, and the
addresses are taken only from ranges reserved for documentation and private
networks, so none of them belongs to anybody.

## License

[GNU Affero General Public License, version 3](LICENSE) (AGPL-3.0-only), with
one additional term in [NOTICE](NOTICE) — © White Ravens

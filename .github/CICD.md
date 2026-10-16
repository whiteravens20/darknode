# CI and CD

## Workflows

| Workflow | Trigger | What it does |
|---|---|---|
| `test.yml` | push and pull request on `main` and `dev` | Server: formatting, tidy module files, vet, build, and the tests with the race detector against a real PostgreSQL. Client: formatting, type check, build. Image: builds it and checks it holds no shell or package manager and does not run as root. |
| `security.yml` | push and pull request on `main` and `dev`; weekly | govulncheck and staticcheck on the server, npm audit and signature checks on the client, dependency review on pull requests, Trivy over the tree and over the image, and the two checks of the pinned actions. |
| `codeql.yml` | push and pull request on `main` and `dev`; weekly | CodeQL over the Go and the TypeScript code. |
| `scorecard.yml` | push to the default branch; weekly | OpenSSF Scorecard, published for the badge in the README. |
| `branch-protection-audit.yml` | daily | Reports when the protection rules on `main` drift from what they should be. |
| `dependabot-auto-merge.yml` | pull request | Merges a Dependabot patch bump once the required checks pass, and labels a major for review. |

There is no release workflow yet. The project is in early development and tags
no releases; the workflow comes with the first one, so that no tag pushed by
accident can publish anything.

## The database in CI

The server's tests use a real PostgreSQL, because what several of them check
is the schema and its constraints: a unique index, a foreign key, a check.
`test.yml` runs one as a service and sets `DARKNODE_TEST_DATABASE_URL`.

Each test makes a database of its own and drops it afterwards, so the tests
do not see each other's rows and can run in parallel.

`DARKNODE_TEST_REQUIRE_DB` is set in CI. Without it a missing database makes
those tests skip, which is right on a laptop and wrong in CI: there, a run
with no database has to fail rather than pass untested.

## Pinned actions

Every action a workflow uses is pinned to a commit SHA, with the version it
stands for in a comment: `actions/checkout@<sha> # v7.0.1`. A tag can be moved
by whoever publishes the action; a SHA cannot. Dependabot keeps the pins
current by rewriting the SHA and the comment together: minor and patch updates
arrive as one pull request a month, a major on its own.

Two jobs in `security.yml` run `.github/scripts/verify-pins.sh`. `Pinned
actions` fails when a reference is not a SHA, has no version comment, or is
not the commit its version tag points at. `Actions audit` asks GitHub's
advisory database about every pinned version, because Dependabot raises no
alert for an action pinned by SHA.

When adding an action, pin it the same way and run the script before pushing.
Never write a SHA from memory: resolve it from the tag.

## Pinned base images

The Dockerfile pins its images by exact version and digest, for example
`golang:1.27.2-alpine@sha256:…`. Docker pulls by the digest; the version
beside it says what the digest is. Dependabot moves both when a new minor or
patch of that line is out, and the digest alone when the same version is
rebuilt. A new major is not proposed: that is a change to the project, made by
hand together with the versions the workflows name.

## The tools the checks run

`staticcheck` and `govulncheck` are pinned in `tools/go.mod`, a module of its
own, so they do not become dependencies of the server. A tool cannot analyse a
module it is not part of, so the workflows build it from `tools/` and then run
it from the repository root.

## What the image is checked for

The stage that ships is `FROM scratch`: one static binary, the world's data
files, the built client, and the licence. `test.yml` proves that it has no
shell and no package manager, and that it does not run as root. Those are the
controls that make a flaw in the game lead nowhere, so they are tested rather
than described.

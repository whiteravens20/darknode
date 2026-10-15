# CI/CD

## Workflows

| Workflow | Trigger | Purpose |
|---|---|---|
| `test.yml` | Push/PR to `main`, `dev` | Lint, typecheck, build, unit tests, Docker build validation |
| `security.yml` | Push/PR to `main`, `dev`; weekly | npm audit, dependency review (one PR summary comment: vulnerabilities, licenses, OpenSSF Scorecard), Trivy filesystem + Docker image scan, checks of the pinned actions |
| `codeql.yml` | Push/PR to `main`, `dev`; weekly | CodeQL static analysis |
| `scorecard.yml` | Push to the default branch; weekly | OpenSSF Scorecard, published for the README badge |
| `docker-snapshot.yml` | Run by hand from `dev` or `main` | Build a snapshot image, push it by digest, Trivy scan, then tag it (`:dev` / `:edge`) |
| `release.yml` | Push tag `vX.Y.Z` from `main` | Build the production image, push it by digest, Trivy CVE scan, then tag it and publish the release |

All PRs to `main` and `dev` require all `test.yml` jobs to pass.

## Pinned actions

Every action a workflow uses is pinned to a commit SHA, with the version it stands for in a comment: `actions/checkout@<sha> # v7.0.1`. A tag can be moved by the action's publisher; a SHA cannot. Dependabot keeps the pins current by rewriting the SHA and the comment together: minor and patch updates arrive as one PR a month, a major on its own.

Two checks in `security.yml` run `.github/scripts/verify-pins.sh`. `Pinned actions` fails when a reference is not a SHA, has no version comment, or is not the commit its version tag points at. `Actions audit` asks GitHub's advisory database about every pinned version, because Dependabot raises no alert for an action pinned by SHA.

When adding an action, pin it the same way and run the script before pushing.

## Pinned base image

The Dockerfiles pin their base image by version and digest: `node:24.21.0-alpine@sha256:…`. Docker pulls the image by the digest; the version beside it says what the digest is. Dependabot moves both when a new minor or patch of that Node line is out, and the digest alone when the same version is rebuilt, each after the same seven days as any other update. A new major of Node is not proposed: it is changed by hand, together with `node-version` in the workflows.

## Branch Protection Rules

Go to **Settings → Branches → Add rule** for `main` (repeat for `dev`):

| Setting | Value | Why |
|---|---|---|
| Require a pull request before merging | ✅ Enabled | No direct pushes to main |
| Require status checks to pass | `backend`, `frontend`, `docker` | All three `test.yml` jobs must be green |
| Require branches to be up to date | ✅ Enabled | Prevents stale-branch merges that skip CI |
| Require review from Code Owners | ✅ Enabled | Enforces `.github/CODEOWNERS` sign-off |
| Do not allow bypassing the above rules | ✅ Enabled | Prevents admins from force-merging |
| Restrict who can push to matching branches | Yourself / release bot only | Reduce blast radius of compromised tokens |

**Optional — require signed commits:**

Enabling _Require signed commits_ in the same branch rule means every commit on `main` must carry a verified GPG or SSH signature. To set up GPG signing locally:

```bash
# Generate a key (use the same email as your GitHub account)
gpg --full-generate-key

# Tell Git to use it
git config --global user.signingkey <KEY_ID>
git config --global commit.gpgsign true

# Export and upload the public key to GitHub → Settings → SSH and GPG keys
gpg --armor --export <KEY_ID>
```

With GitHub's _Vigilant Mode_ (Settings → SSH and GPG keys → Vigilant mode), unverified commits are shown with a ⚠️ warning in the UI, providing an additional visual audit trail.

## Snapshot Images

The Snapshot build workflow, run by hand from `dev` or `main`, builds an image, pushes it to GHCR by its digest alone and scans it; the tags are put on it after the scan:

| Branch | Tags | Use case |
|---|---|---|
| `dev` | `:dev`, `:dev-<sha>` | Test development builds locally or on a staging host |
| `main` | `:edge`, `:edge-<sha>` | Preview what the next release will look like |

Pull a snapshot:

```bash
# Latest dev build
docker pull ghcr.io/whiteravens20/archivum-null:dev

# Specific commit (pin to a known-good snapshot)
docker pull ghcr.io/whiteravens20/archivum-null:dev-a1b2c3d
```

A snapshot of `dev` is scanned in report-only mode (findings visible in the Security tab) and tagged whatever the scan found. A snapshot of `main` is tagged only when its scan found no unacknowledged HIGH/CRITICAL CVE.

## Creating a Release

```bash
# Ensure you are on main and it is green, then tag:
git checkout main
git tag v1.2.3
git push origin v1.2.3
```

> Tags pushed from `dev` or any other branch are silently ignored by `release.yml`. Only tags whose base ref is `main` trigger a production build.

This triggers `release.yml`, which:
1. Builds the production Docker image (no build-time constants — all config is read from `.env` at runtime via `GET /api/config`)
2. Pushes it to `ghcr.io/whiteravens20/archivum-null` by its digest alone, with SBOM and provenance attestations (visible in the package's attestations tab)
3. Scans that digest with Trivy — fails the workflow on unacknowledged HIGH/CRITICAL CVEs
4. Only when the scan passed: puts the tags `:1.2.3`, `:1.2`, `:1`, `:main` and `:latest` on it, checks that each names the scanned digest, and publishes the GitHub Release

## Required GitHub Repository Configuration

No repository variables are needed for image builds. All runtime configuration
(`MAX_FILE_SIZE`, `TURNSTILE_SITE_KEY`, `DEFAULT_TTL`, etc.) is read from the
operator's `.env` file at container start and served to the frontend at runtime
via `GET /api/config`. Nothing is baked into the image at build time.

The only secret the workflows use is `GITHUB_TOKEN`, which GitHub provides
automatically — no configuration needed.

## Supply-Chain Hardening

The 7-day release-age policy is enforced in three layers, each covering what the
one before it cannot.

### 1. Dependabot cooldown — the primary control

`.github/dependabot.yml` sets `cooldown: default-days: 7` on every ecosystem.
Dependabot will not propose a version until it has been public for a week, so a
too-fresh version never becomes a PR and therefore can never be merged early —
including by `dependabot-auto-merge.yml`, which merges patch bumps
automatically once CI is green.

Cooldown covers **version** updates only. Dependabot **security** updates are
exempt by design and open the moment an advisory lands; that is intended — a
known-exploitable dependency should not wait out a quarantine window.

Do **not** reintroduce a `min-release-age` PR label to cover this. A label is
not a gate, so auto-merge walks past it, and labelling a security PR as
quarantined points the maintainer the wrong way.

### 2. `.npmrc` — the client-side backstop

Every `.npmrc` (root and each workspace) enforces:

| Setting | Value | Effect |
|---|---|---|
| `ignore-scripts` | `true` | Blocks `postinstall` / `preinstall` scripts from dependencies. Prevents malicious packages from executing code at install time (e.g. the axios 1.14.1 incident). |
| `min-release-age` | `7` | Refuses to resolve any package version published less than 7 days ago. |

`min-release-age` fires at **resolution** — `npm install`, `npm update` — which
in practice means a human adding or bumping a package by hand. It does **not**
apply to `npm ci`, which installs exactly what the lockfile pins. That is why it
cannot be the primary control, and why cooldown exists above it.

`ignore-scripts` applies everywhere:

- **CI workflows** (`test.yml`, `codeql.yml`, `security.yml`) — `npm ci --ignore-scripts`
- **Dockerfiles** (`Dockerfile`, `Dockerfile.dev`) — `npm ci --ignore-scripts`
- **docker-compose.dev.yml** — `npm install --ignore-scripts`

If a trusted dependency genuinely requires lifecycle scripts (e.g. `esbuild`,
`sharp`), rebuild it explicitly after install:

```bash
npm ci --ignore-scripts
npm rebuild esbuild   # only for packages you trust
```

### 3. CI verification — what actually shipped

`security.yml` re-installs each workspace and runs `npm audit signatures`,
verifying every installed tarball against its registry signature. This is the
only place signature verification happens; there is **no** npmrc setting for it.
`audit-signatures` is a flag on the `npm audit` command — putting it in
`.npmrc` makes npm warn `Unknown project config` and changes nothing about
how packages install.

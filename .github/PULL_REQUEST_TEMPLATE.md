## What this changes

What the change does, and why.

## Type of change

- [ ] Fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation
- [ ] Build, CI or tooling

## Related issue

Closes #

## How it was tested

What you ran, and what you saw.

- [ ] `go test ./...` passes, with a database (`DARKNODE_TEST_DATABASE_URL`)
- [ ] `npm run typecheck` and `npm run build` pass in `web/`
- [ ] Tried in a browser, if the change touches the page

## Checklist

See [CONTRIBUTING.md](../CONTRIBUTING.md) before you open this.

- [ ] `gofmt` is clean and `go vet ./...` passes
- [ ] New behaviour is covered by a test that fails without the change
- [ ] Commits are Conventional Commits, one topic each, and signed
- [ ] Documentation is updated where the change makes it wrong

## If it touches the game's rules

- [ ] The server decides the outcome; the browser only shows it and sends commands
- [ ] Numbers that change the balance are named here

## If it touches accounts or the database

- [ ] A schema change is a new migration, never an edit to one already applied
- [ ] No new personal data is asked for or stored
- [ ] A secret stays out of the log, out of the page, and out of the image

## If it touches the image or the compose file

- [ ] The stage that ships still has no shell and no package manager
- [ ] Base images stay pinned by version and digest
- [ ] It still runs as a user that is not root, on a read-only filesystem

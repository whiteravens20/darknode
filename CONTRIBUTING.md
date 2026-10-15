# Contributing to DarkNode

Thank you for considering a contribution to DarkNode. Please read this guide before opening a pull request.

## Before You Start

- Check the [open issues](../../issues) and [pull requests](../../pulls) to avoid duplicating work.
- For anything larger than a bug fix, open an issue first and agree on the approach.
- By contributing, you agree to the project [License](LICENSE) and [Code of Conduct](CODE_OF_CONDUCT.md).

## Scope of Contributions

<!-- TODO for the maintainer: say what this project welcomes and what it will not accept, with the reason for each refusal. -->

**Welcome:** bug fixes, tests, documentation and accessibility improvements.

**Not accepted:** changes that weaken a security control, and features that collect data about the people who use the project.

## Development Setup

<!-- TODO for the maintainer: the requirements and the exact commands that install, run and build this project. -->

## Project Structure

<!-- TODO for the maintainer: a short map of the source tree, one line per directory. -->

## Coding Guidelines

### General

- Keep a change focused: one logical change per pull request.
- No dead code or commented-out blocks in a submitted pull request.

### Dependencies

- A new dependency needs its reason in the pull request description.
- Pin versions and commit the lockfile.

### Commits

Use [Conventional Commits](https://www.conventionalcommits.org/) style, one topic per commit:

```
feat: add X
fix: handle Y
docs: describe Z
```

Commits must be signed.

## Testing Requirements

<!-- TODO for the maintainer: the commands that run lint, type checks and tests here, and what a change is expected to add. -->

Every check must pass with zero failures before you submit.

## Secure Contributing

### Input handling

- Validate input at the boundary where it enters, and treat everything from outside as untrusted.

### Dependencies

- Run the dependency audit before submitting and name any finding in the pull request.

### Secrets and credentials

- Never commit secrets, credentials, tokens or private keys: not in code, not in comments, not in test fixtures.
- If you commit one by accident, treat it as compromised and rotate it at once. Then open a private security report.

### AI-assisted code

Most of this project is written with AI coding tools, as the [README](README.md#how-the-code-is-written-and-checked) describes, and contributions may be too. Do not submit AI output that you cannot explain and defend in review: read it, test it and take responsibility for it.

<!-- TODO for the maintainer: the rules this project's own risks call for, such as what must never be logged or what must stay on the client. -->

## Submitting Changes

1. Branch from `dev`.
2. Make the change, with tests.
3. Open a pull request into `dev` and fill in the template.
4. CI must pass before the pull request is merged.

## Reporting Security Vulnerabilities

Do not open a public issue for a security bug. See [SECURITY.md](SECURITY.md).

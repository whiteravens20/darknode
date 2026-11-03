// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { existsSync } from 'node:fs';
import { defineConfig, devices } from '@playwright/test';

// The test drives a whole instance: this configuration starts the real server
// against a real PostgreSQL and serves the built client to a real browser.
// Nothing is stubbed, because what the test checks is that the three fit
// together — the one thing the server's own tests cannot see.

const port = 8099;
const baseURL = `http://127.0.0.1:${port}`;

const database = process.env.DARKNODE_E2E_DATABASE_URL;
if (!database) {
  throw new Error(
    'DARKNODE_E2E_DATABASE_URL is not set. Point it at a PostgreSQL this test may write to, ' +
      'for example postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable',
  );
}
if (!existsSync(new URL('../web/dist/index.html', import.meta.url))) {
  throw new Error(
    'The client is not built. Run: npm --prefix ../web ci --ignore-scripts && npm --prefix ../web run build',
  );
}

export default defineConfig({
  testDir: 'tests',
  // Registration is slow on purpose: the browser solves a puzzle and the
  // server refuses a solution that comes back too fast.
  timeout: 90_000,
  expect: { timeout: 15_000 },
  // One instance, one world: the tests share it and run one at a time.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [['github'], ['html', { open: 'never' }]]
    : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    // `go run` keeps one command for the local run and for CI; Go's build
    // cache makes the second start quick.
    command: 'go run ./cmd/darknode serve',
    cwd: '..',
    url: `${baseURL}/healthz`,
    timeout: 180_000,
    reuseExistingServer: false,
    stdout: 'pipe',
    stderr: 'pipe',
    env: {
      DARKNODE_LISTEN: `127.0.0.1:${port}`,
      DARKNODE_DATABASE_URL: database,
      // A test secret, and only ever that: a real instance makes its own and
      // keeps it in a file. This one is in the repository on purpose, so that
      // nobody is tempted to reuse a real one here.
      DARKNODE_SECRET: 'darknode-end-to-end-test-secret-not-for-real-use',
      DARKNODE_COOKIE_SECURE: 'false',
      DARKNODE_WEB_DIR: 'web/dist',
      DARKNODE_REGISTRATION: 'open',
      // The puzzle stays real, at a difficulty a browser clears in a moment.
      DARKNODE_POW_BITS: '12',
      DARKNODE_LOG_LEVEL: 'info',
    },
  },
});

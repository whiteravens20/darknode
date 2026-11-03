// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

// What a new player does, through the interface a player uses: make an
// account, keep the recovery codes, start a run, read it, sign out, come back
// and find the same run. The prototype this game replaces had tests that
// passed while exactly this path was broken, so nothing here reaches behind
// the browser — no API call of its own, no database of its own, no stubs.

const password = 'a-password-long-enough';

/** agentName returns a name no other run of this test has taken. */
function agentName(): string {
  return `e2e${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

/** command types one line into the terminal and sends it. */
async function command(page: Page, line: string): Promise<void> {
  const input = page.getByLabel('Command');
  await input.fill(line);
  await input.press('Enter');
}

test('an agent signs up, starts a run, reads it, and comes back to it', async ({ page }) => {
  const agent = agentName();

  await test.step('the instance serves its sign-in page', async () => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'DarkNode', level: 1 })).toBeVisible();
  });

  await test.step('registration hands out eight recovery codes', async () => {
    await page.getByRole('button', { name: 'New agent' }).click();
    await page.getByLabel('Agent name').fill(agent);
    await page.getByLabel('New password').fill(password);
    await page.locator('form').getByRole('button', { name: 'Create the account' }).click();

    await expect(page.getByRole('heading', { name: 'Your recovery codes' })).toBeVisible();
    await expect(page.getByRole('listitem')).toHaveCount(8);

    await page.getByRole('checkbox').check();
    await page.getByRole('button', { name: 'Start playing' }).click();
  });

  await test.step('the state stream opens and reports no run yet', async () => {
    await expect(page.getByText(agent, { exact: true })).toBeVisible();
    // The dot's tooltip says the stream is live, which it can only be once
    // the server has sent a state over it.
    await expect(page.getByTitle('Connected to the server')).toBeVisible();
    await expect(page.getByText(/no run/)).toBeVisible();
  });

  await test.step('`run new` starts a run', async () => {
    await command(page, 'run new');
    await expect(page.getByText('New run started. Your gateway is online.')).toBeVisible();
    // The status bar changes because the server pushed a new state, not
    // because the page worked anything out for itself.
    await expect(page.getByText('rep 0')).toBeVisible();
  });

  await test.step('`status` prints what the run holds', async () => {
    await command(page, 'status');
    await expect(page.getByText('STATUS', { exact: true })).toBeVisible();
    await expect(page.getByText(new RegExp(`Agent\\s+${agent}`))).toBeVisible();
    await expect(page.getByText(/Balance\s+\$\d+ clean, \$0 dirty/)).toBeVisible();
    await expect(page.getByText(/Links\s+\d+ known servers/)).toBeVisible();
  });

  await test.step('signing out ends the session', async () => {
    await command(page, 'logout');
    await expect(page.getByRole('heading', { name: 'DarkNode', level: 1 })).toBeVisible();
  });

  await test.step('signing in again finds the same run', async () => {
    await page.getByLabel('Agent name').fill(agent);
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.locator('form').getByRole('button', { name: 'Sign in' }).click();

    await expect(page.getByText(agent, { exact: true })).toBeVisible();
    await expect(page.getByText('rep 0')).toBeVisible();
    await command(page, 'status');
    await expect(page.getByText(/Balance\s+\$\d+ clean, \$0 dirty/)).toBeVisible();
  });
});

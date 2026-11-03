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

  await test.step('the world clock moves on its own', async () => {
    // Nothing is typed in this step. The clock changes because the server's
    // simulation loop broadcasts it and the page applies the delta — which is
    // the whole of the state stream working, from the loop to the pixel.
    const clock = page.getByTitle('World time');
    const first = (await clock.textContent()) ?? '';
    expect(first).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
    await expect(clock).not.toHaveText(first, { timeout: 20_000 });
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

  await test.step('a server tells you nothing until it is probed', async () => {
    await command(page, 'servers');
    await expect(page.getByText(/172\.16\.10\.1\s+Meridian Savings\s+not probed/)).toBeVisible();

    await command(page, 'probe 172.16.10.1');
    await expect(page.getByText(/Type\s+BANK/)).toBeVisible();
    await expect(page.getByText(/Security\s+6/)).toBeVisible();
    await expect(page.getByText(/Layers\s+proxy \+ firewall \+ password \+ monitor/)).toBeVisible();
  });

  await test.step('a route is built and the register is read where it stands', async () => {
    await command(page, 'bounce add 10.0.0.1');
    await expect(page.getByText(/added to the route/)).toBeVisible();

    await command(page, 'connect 10.10.10.10');
    // The status bar follows the connection because the server pushed it, not
    // because the page worked it out.
    await expect(page.getByTitle('Connection')).toHaveText(/root on InterNIC Registry/);

    await command(page, 'links');
    await expect(page.getByText(/THE REGISTER/)).toBeVisible();
    await expect(page.getByText(/Federal Administration/)).toBeVisible();

    await command(page, 'disconnect');
    await expect(page.getByTitle('Connection')).toHaveText('not connected');
  });

  await test.step('a program breaks into the practice machine', async () => {
    // The practice machine was a relay a moment ago; a server cannot be a
    // relay of its own route.
    await command(page, 'bounce clear');
    await command(page, 'connect 10.0.0.1');
    await expect(page.getByTitle('Connection')).toHaveText(/^on DarkNode Practice Machine/);

    await command(page, 'run pass_breaker');
    // The progress comes from the simulation loop, which gives the program a
    // share of the gateway's processor ten times a second.
    await expect(page.getByTitle('Running programs')).toContainText('PassBreaker');
    // Security 1 at 2000 MHz is two seconds of work, and then the password
    // layer is open.
    await expect(page.getByTitle('Connection')).toHaveText(/^root on DarkNode Practice Machine/, {
      timeout: 20_000,
    });
    await expect(page.getByTitle('Running programs')).toBeHidden();

    await command(page, 'status');
    await expect(page.getByText(/Layers\s+password open/)).toBeVisible();
    await command(page, 'disconnect');
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

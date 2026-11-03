// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/** password is what every test agent signs up with. */
export const password = 'a-password-long-enough';

/** agentName returns a name no other run of these tests has taken. */
export function agentName(): string {
  return `e2e${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

/** command types one line into the terminal and sends it. */
export async function command(page: Page, line: string): Promise<void> {
  const input = page.getByLabel('Command');
  await input.fill(line);
  await input.press('Enter');
}

/**
 * signUp makes an agent and starts a run, which is where every test of the
 * game itself begins. The sign-up puzzle is solved in the browser and the
 * server will not take a solution that comes back too fast, so this takes a
 * couple of seconds by design.
 */
export async function signUp(page: Page): Promise<string> {
  const agent = agentName();
  await page.goto('/');
  await page.getByRole('button', { name: 'New agent' }).click();
  await page.getByLabel('Agent name').fill(agent);
  await page.getByLabel('New password').fill(password);
  await page.locator('form').getByRole('button', { name: 'Create the account' }).click();

  await expect(page.getByRole('heading', { name: 'Your recovery codes' })).toBeVisible();
  await page.getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Start playing' }).click();

  await expect(page.getByText(agent, { exact: true })).toBeVisible();
  await command(page, 'run new');
  await expect(page.getByText('New run started. Your gateway is online.')).toBeVisible();
  return agent;
}

/** terminal is everything the terminal has printed. */
export async function terminal(page: Page): Promise<string> {
  return (await page.locator('.cursor-text').innerText()) ?? '';
}

/**
 * breakIn opens the password layer of the server the player is connected to
 * and waits for root. Nothing else is in the way of the machines these tests
 * use, which is what makes them the first ones a new agent can take.
 */
export async function breakIn(page: Page): Promise<void> {
  await command(page, 'run pass_breaker');
  await expect(page.getByTitle('Connection')).toHaveText(/^root on /, { timeout: 30_000 });
}

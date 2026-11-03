// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { expect, test } from '@playwright/test';
import { breakIn, command, signUp, terminal } from './helpers';

// The milestone's own exit criterion, the clean half: take a contract, go in
// through a relay, do the work, clear the lines on the target and on the
// relay, leave, and be paid. Nothing here reaches behind the browser.
test('a contract is taken, done cleanly, and paid', async ({ page }) => {
  await signUp(page);

  let target = '';
  let file = '';
  let steal = true;

  await test.step('a contract comes with the address of its target', async () => {
    await command(page, 'missions');
    await expect(page.getByText(/WORK/)).toBeVisible();

    await command(page, 'mission accept 1');
    await expect(page.getByText(/Taken on:/)).toBeVisible();
    const taken = /Taken on: (steal|destroy) (\S+) on .*? at (\d+\.\d+\.\d+\.\d+)/.exec(
      await terminal(page),
    );
    expect(taken, 'the contract says what to do and where').not.toBeNull();
    steal = taken?.[1] === 'steal';
    file = taken?.[2] ?? '';
    target = taken?.[3] ?? '';
    expect(file).toMatch(/^\//);
    expect(target).toMatch(/^\d+\./);
  });

  await test.step('the way in goes through a relay', async () => {
    // The practice machine is an address every agent starts with, and a
    // relay is what gives a trace ground to cover.
    await command(page, 'bounce add 10.0.0.1');
    await expect(page.getByText(/added to the route/)).toBeVisible();

    await command(page, `connect ${target}`);
    await expect(page.getByTitle('Connection')).toHaveText(/^on /);
    await breakIn(page);
  });

  await test.step('the work is done', async () => {
    if (steal) {
      await command(page, `cp ${file}`);
      await expect(page.getByText(new RegExp(`${file} is on your gateway`))).toBeVisible({
        timeout: 30_000,
      });
    } else {
      await command(page, `rm ${file}`);
      await expect(page.getByText(new RegExp(`${file} is gone`))).toBeVisible();
    }
  });

  await test.step('the lines are cleared on the target and on the relay', async () => {
    await command(page, 'log wipe');
    await expect(page.getByText(/cleared with LogWiper v1/)).toBeVisible();
    await command(page, 'disconnect');

    // A route has to be cleaned hop by hop, and that means root on the hop.
    await command(page, 'bounce clear');
    await command(page, 'connect 10.0.0.1');
    await breakIn(page);
    await command(page, 'log wipe');
    await expect(page.getByText(/cleared with LogWiper v1/).last()).toBeVisible();
    await command(page, 'disconnect');
  });

  await test.step('the contract pays', async () => {
    await command(page, 'mission submit 1');
    await expect(page.getByText(/Delivered\./)).toBeVisible();

    // The money is on the account, which the status bar reads from the
    // server, not from the page.
    await command(page, 'bank');
    const money = /\$([\d,]+)/.exec(await terminal(page));
    expect(money, 'the bank says what is in the account').not.toBeNull();
    expect(Number((money?.[1] ?? '0').replace(/,/g, ''))).toBeGreaterThan(100);
  });
});

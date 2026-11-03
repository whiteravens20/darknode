// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { expect, test } from '@playwright/test';
import { breakIn, command, signUp, terminal } from './helpers';

// The milestone's exit criterion, the dirty half: the same break-in with the
// lines left where they were. Nobody says anything at the time — that is the
// point — and then the bill arrives, on the clock of the player's own time.
//
// The branch terminal of the player's own bank is the machine for it: weak
// enough for a starting kit, and owned by the kind of organisation that reads
// its logs the same day.
test('a break-in left in the logs comes back as a fine', async ({ page }) => {
  await signUp(page);

  await test.step('the branch terminal is taken, with a relay in the way', async () => {
    await command(page, 'probe 172.16.10.9');
    await expect(page.getByText(/Type\s+BANK/)).toBeVisible();
    await expect(page.getByText(/Security\s+2/)).toBeVisible();

    await command(page, 'bounce add 10.0.0.1');
    await command(page, 'connect 172.16.10.9');
    await breakIn(page);
  });

  await test.step('the day book is taken and nothing is cleared', async () => {
    await command(page, 'cp /data/daybook.db');
    await expect(page.getByText(/daybook\.db is on your gateway/)).toBeVisible({ timeout: 30_000 });

    // The lines stand, which is the whole difference from the clean run.
    await command(page, 'log');
    await expect(page.getByText(/COPY/)).toBeVisible();
    await expect(page.getByText(/stands/).first()).toBeVisible();

    await command(page, 'disconnect');
    await expect(
      page.getByText(/Disconnected from Meridian Savings, Branch Terminal/),
    ).toBeVisible();
  });

  await test.step('the bill arrives while the player goes on playing', async () => {
    // A bank reads its log within twelve minutes of the game's clock, which
    // is twelve seconds of play; the walk then costs the relay's hop. The
    // player is told nothing until the money is taken.
    await expect(page.getByText(/A fine of \$\d+/)).toBeVisible({ timeout: 60_000 });

    await command(page, 'bank');
    const owed = /-([\d,]+)/.exec(await terminal(page));
    expect(owed, 'the account is in the red').not.toBeNull();
  });
});

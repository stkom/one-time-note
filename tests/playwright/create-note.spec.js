import {expect, test} from '@playwright/test';

const noteId = 'A'.repeat(43);
const burnToken = Buffer.alloc(32, 5).toString('base64url');

test('does not expose a generated link after the page is hidden', async ({page}) => {
    let pendingSave;
    let saveStarted;
    const saveRequest = new Promise(resolve => {
        saveStarted = resolve;
    });
    await mockCreateTicket(page);
    await page.route('**/api/notes/**', route => {
        pendingSave = route;
        saveStarted();
    });

    await startSave(page);
    await saveRequest;
    await page.evaluate(() => window.dispatchEvent(new Event('pagehide')));
    await pendingSave.fulfill({
        status: 201,
        json: {id: noteId, expiresInSeconds: 86400},
    });

    await expect(page.getByLabel('Encrypted note link')).toHaveValue('');
    await expect(page.locator('#create-note-result')).toBeHidden();
});

test('rejects an invalid ticket response', async ({page}) => {
    await page.route('**/api/tickets', route => route.fulfill({
        status: 200,
        json: {id: 'invalid', ticket: 'ticket', burnToken},
    }));

    await startSave(page);

    await expect(page.locator('#create-note-error')).toContainText(
        'The server returned an invalid note ticket.'
    );
});

test('rejects an invalid save response', async ({page}) => {
    await mockCreateTicket(page);
    await page.route('**/api/notes/**', route => route.fulfill({
        status: 201,
        json: {id: 'invalid'},
    }));

    await startSave(page);

    await expect(page.locator('#create-note-error')).toContainText(
        'The server returned an invalid note link.'
    );
    await expect(page.locator('#create-note-result')).toBeHidden();
});

test('shows a safe message when saving fails', async ({page}) => {
    await mockCreateTicket(page);
    await page.route('**/api/notes/**', route => route.fulfill({
        status: 400,
        json: {error: 'ticket_unusable'},
    }));

    await startSave(page);

    await expect(page.locator('#create-note-error')).toContainText(
        'This note ticket is no longer valid. Try again.'
    );
    await expect(page.locator('#create-note-result')).toBeHidden();
});

async function mockCreateTicket(page) {
    await page.route('**/api/tickets', route => route.fulfill({
        status: 200,
        json: {
            id: noteId,
            ticket: 'fresh-ticket',
            burnToken,
        },
    }));
}

async function startSave(page) {
    await page.goto('/');
    await page.getByRole('textbox', {name: 'New encrypted note'}).fill('Secret note');
    await page.getByRole('button', {name: 'Save & Encrypt'}).click();
}

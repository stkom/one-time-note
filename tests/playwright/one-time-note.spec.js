import {expect, test} from '@playwright/test';

test('creates, opens, and consumes a one-time encrypted note', async ({page, context}) => {
    const consoleIssues = [];
    collectConsoleIssues(page, consoleIssues);

    await page.goto('/');
    await expect(page).toHaveTitle('One Time Note');
    await expect(page.getByText('New encrypted note')).toBeVisible();
    await expect(page.getByRole('button', {name: 'Save & Encrypt'})).toBeEnabled();

    await page.getByRole('textbox', {name: 'New encrypted note'}).fill('browser test secret');
    await page.getByRole('button', {name: '1 day'}).click();
    await page.getByRole('option', {name: '3 days'}).click();
    await page.getByRole('button', {name: 'Save & Encrypt'}).click();

    const linkField = page.getByLabel('Encrypted note link');
    await expect(linkField).toBeVisible();
    await expect(linkField).toBeFocused();
    await expect(page.getByText('Opens once')).toBeVisible();
    await expect(page.getByText('Expires after 3 days')).toBeVisible();
    await expect(page.getByText('Secret is stored only in this URL')).toBeVisible();

    const noteURL = await linkField.inputValue();
    const parsedURL = new URL(noteURL);
    expect(parsedURL.pathname).toMatch(/^\/note\/[A-Za-z0-9_-]{43}$/);
    expect(parsedURL.hash).toMatch(/^#v1\./);

    const firstOpen = await context.newPage();
    collectConsoleIssues(firstOpen, consoleIssues);
    await firstOpen.goto(noteURL);
    await expect(firstOpen).toHaveURL(url => url.hash.startsWith('#v1.'));
    await expect(firstOpen.getByText('Opening this note will destroy it on the server.')).toBeVisible();
    await firstOpen.reload();
    await expect(firstOpen).toHaveURL(url => url.hash.startsWith('#v1.'));
    await firstOpen.getByRole('button', {name: 'View Note'}).click();
    await expect(firstOpen).toHaveURL(url => url.hash === '');

    const noteText = firstOpen.getByLabel('Your note');
    await expect(noteText).toHaveValue('browser test secret');
    await expect(noteText).toBeFocused();

    const secondOpen = await context.newPage();
    collectConsoleIssues(secondOpen, consoleIssues);
    await secondOpen.goto(noteURL);
    await expect(secondOpen).toHaveURL(url => url.hash.startsWith('#v1.'));
    await secondOpen.getByRole('button', {name: 'View Note'}).click();
    await expect(secondOpen).toHaveURL(url => url.hash === '');
    await expect(secondOpen.getByText(/could not be opened/i)).toBeVisible();

    expect(consoleIssues).toEqual([]);
});

test('Cancel scrubs the history entry before Back', async ({page, context}) => {
    const noteURL = await createNote(page, 'cancelled browser secret');
    const recipient = await context.newPage();
    let openRequests = 0;
    recipient.on('request', request => {
        if (request.url().includes('/api/notes/') && request.url().endsWith('/open')) {
            openRequests += 1;
        }
    });

    await recipient.goto(noteURL);
    await expect(recipient).toHaveURL(url => url.hash.startsWith('#v1.'));
    await recipient.getByRole('link', {name: 'Cancel'}).click();
    await expect(recipient).toHaveURL('/');

    await recipient.goBack();
    await expect(recipient).toHaveURL(url => url.pathname.startsWith('/note/') && url.hash === '');
    await recipient.getByRole('button', {name: 'View Note'}).click();

    await expect(recipient.getByText('No note secret was found in the URL.')).toBeVisible();
    expect(openRequests).toBe(0);
});

test('malformed routes scrub recognized note fragments', async ({page}) => {
    await page.goto('/not-a-note#v1.malformed-secret');

    await expect(page).toHaveURL(url => url.pathname === '/not-a-note' && url.hash === '');
    await expect(page.getByRole('heading', {name: '404 Not Found'})).toBeVisible();
});

async function createNote(page, plaintext) {
    await page.goto('/');
    await page.getByRole('textbox', {name: 'New encrypted note'}).fill(plaintext);
    await page.getByRole('button', {name: 'Save & Encrypt'}).click();
    const linkField = page.getByLabel('Encrypted note link');
    await expect(linkField).toBeVisible();
    return linkField.inputValue();
}

function collectConsoleIssues(page, issues) {
    page.on('console', msg => {
        if (msg.text().includes('Failed to load resource: the server responded with a status of 404')) {
            return;
        }
        if (['error', 'warning'].includes(msg.type())) {
            issues.push(`${msg.type()}: ${msg.text()}`);
        }
    });
    page.on('pageerror', error => {
        issues.push(`pageerror: ${error.message}`);
    });
}

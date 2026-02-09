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
    await expect(firstOpen).toHaveURL(url => url.hash === '');
    await expect(firstOpen.getByText('Opening this note will destroy it on the server.')).toBeVisible();
    await firstOpen.getByRole('button', {name: 'View Note'}).click();

    const noteText = firstOpen.getByLabel('Your note');
    await expect(noteText).toHaveValue('browser test secret');
    await expect(noteText).toBeFocused();

    const secondOpen = await context.newPage();
    collectConsoleIssues(secondOpen, consoleIssues);
    await secondOpen.goto(noteURL);
    await expect(secondOpen).toHaveURL(url => url.hash === '');
    await secondOpen.getByRole('button', {name: 'View Note'}).click();
    await expect(secondOpen.getByText(/could not be opened/i)).toBeVisible();

    expect(consoleIssues).toEqual([]);
});

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

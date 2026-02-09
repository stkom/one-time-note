import {defineConfig} from '@playwright/test';
import {existsSync, rmSync} from 'node:fs';

const port = 18080;
const localChromium = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ?? '/usr/bin/chromium';
const launchOptions = existsSync(localChromium) ? {executablePath: localChromium} : {};
const dbPath = process.env.NOTE_DB_PATH ?? '/tmp/one-time-note-playwright.db';
if (!process.env.NOTE_DB_PATH) {
    rmSync(dbPath, {force: true});
}

export default defineConfig({
    testDir: './tests/playwright',
    timeout: 30_000,
    use: {
        baseURL: `http://127.0.0.1:${port}`,
        browserName: 'chromium',
        launchOptions,
    },
    webServer: {
        command: 'go run . --dev',
        url: `http://127.0.0.1:${port}/healthz`,
        reuseExistingServer: !process.env.CI,
        timeout: 30_000,
        env: {
            GOCACHE: process.env.GOCACHE ?? '/tmp/one-time-note-go-build-cache',
            NOTE_DB_PATH: dbPath,
            NOTE_ENVIRONMENT: 'development',
            NOTE_PORT: String(port),
        },
    },
});

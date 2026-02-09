import {beforeEach, describe, expect, it, vi} from 'vitest';
import {initCreateNotePage} from '../../web/static/create-note.js';
import {decode, encode, parseSecretFragment} from '../../web/static/crypto-utils.js';

const noteId = 'A'.repeat(43);
const burnToken = encode(new Uint8Array(32).fill(5));

describe('create-note page controller', () => {
    beforeEach(() => {
        vi.useRealTimers();
        document.body.innerHTML = createPageHtml();
        window.history.replaceState(null, '', '/');
    });

    it('should encrypt and save a note, then show the generated link', async () => {
        const fetch = vi.fn(async (url, options) => {
            if (url === '/api/tickets') {
                expect(options.method).toBe('POST');
                expect(options.credentials).toBe('same-origin');
                expect(options.cache).toBe('no-store');
                expect(options.redirect).toBe('error');
                expect(options.headers['Accept']).toBe('application/json');
                return createTicketResponse();
            }

            expect(url).toBe(`/api/notes/${noteId}`);
            expect(options.method).toBe('POST');
            expect(options.credentials).toBe('same-origin');
            expect(options.cache).toBe('no-store');
            expect(options.redirect).toBe('error');
            expect(options.headers['Accept']).toBe('application/json');
            expect(options.headers['Content-Type']).toBe('application/json');

            const body = JSON.parse(options.body);
            expect(body.ticket).toBe('fresh-ticket');
            expect(body.burnToken).toBe(burnToken);
            expect(typeof body.payload).toBe('string');
            const payload = JSON.parse(body.payload);
            expect(payload).toEqual({
                protected: expect.stringMatching(/^[A-Za-z0-9_-]+$/),
                iv: expect.stringMatching(/^[A-Za-z0-9_-]+$/),
                ct: expect.stringMatching(/^[A-Za-z0-9_-]+$/),
            });
            expect(JSON.parse(new TextDecoder().decode(decode(payload.protected)))).toEqual({v: 1, alg: 'A256GCM'});
            expect(body.expiresInSeconds).toBe(86400);

            return Response.json({id: noteId, expiresInSeconds: 86400}, {status: 201});
        });

        initCreateNotePage(document, {
            fetch,
            clipboard: {writeText: vi.fn()},
            location: window.location,
            crypto: fixedRandomCrypto(7),
        });

        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        await vi.waitFor(() => expect(document.getElementById('create-note-form').classList.contains('hidden')).toBe(true));

        const link = document.getElementById('note-link').value;
        const url = new URL(link);
        expect(url.origin).toBe('https://notes.example.test');
        expect(url.pathname).toBe(`/note/${noteId}`);
        expect(url.hash).toMatch(/^#v1\./);
        expect(parseSecretFragment(url.hash.slice(1)).burnToken).toBe(burnToken);
        expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('created-expiration-fact').textContent).toBe('Expires after 1 day');
        expect(document.getElementById('status-text').textContent).toBe('Secret stored only in this URL');
        expect(document.getElementById('note-content').value).toBe('');
        expect(document.activeElement).toBe(document.getElementById('note-link'));
    });

    it('should reject empty notes without sending a request', async () => {
        const fetch = vi.fn();

        initCreateNotePage(document, {fetch});
        submitForm();

        expect(fetch).not.toHaveBeenCalled();
        expect(document.getElementById('create-note-error').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('create-note-error-message').textContent).toBe('Enter a note before encrypting.');
        expect(document.activeElement).toBe(document.getElementById('create-note-error'));
    });

    it('should send the selected fixed expiration and display it after save', async () => {
        const fetch = successfulCreateFetch(3 * 24 * 60 * 60, (_url, options) => {
            expect(JSON.parse(options.body).expiresInSeconds).toBe(3 * 24 * 60 * 60);
        });

        initCreateNotePage(document, {
            fetch,
            clipboard: {writeText: vi.fn()},
            crypto: fixedRandomCrypto(7),
        });

        document.getElementById('expiration-toggle').click();
        document.querySelector('[data-expires-in-seconds="259200"]').click();
        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        await vi.waitFor(() => expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(false));
        expect(document.getElementById('created-expiration-fact').textContent).toBe('Expires after 3 days');
        expect(document.getElementById('expiration-selected').textContent).toBe('3 days');
    });

    it('should derive expiration validation from rendered options', async () => {
        const fetch = vi.fn();
        document.getElementById('expiration-menu').innerHTML = `
            <button type="button" class="expiration-option" aria-selected="true" data-expires-in-seconds="7200">2 hours</button>
            <button type="button" class="expiration-option" aria-selected="false" data-expires-in-seconds="10800">3 hours</button>
        `;
        document.getElementById('expires-in-seconds').value = '3600';

        initCreateNotePage(document, {fetch});

        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        expect(fetch).not.toHaveBeenCalled();
        expect(document.getElementById('create-note-error-message').textContent).toBe('Choose an expiration between 2 hours and 3 hours.');
    });

    it('should disable saving when the estimated encrypted note is too large', () => {
        document.getElementById('create-config').dataset.maxNoteSize = '30';

        initCreateNotePage(document, {fetch: vi.fn()});
        expect(document.getElementById('note-size-status').textContent).toBe('Limit 30 bytes');

        document.getElementById('note-content').value = 'this text exceeds the small test limit';
        document.getElementById('note-content').dispatchEvent(new Event('input', {bubbles: true}));

        expect(document.getElementById('save-note-button').disabled).toBe(true);
        expect(document.getElementById('note-size-status').textContent).toBe('38 bytes of 30 bytes');
        expect(document.getElementById('note-size-status').classList.contains('helper-text-danger')).toBe(true);
    });

    it('should support keyboard save for drafts', async () => {
        const fetch = successfulCreateFetch();

        initCreateNotePage(document, {
            fetch,
            clipboard: {writeText: vi.fn()},
            crypto: fixedRandomCrypto(7),
        });

        document.getElementById('note-content').value = 'save me';
        document.getElementById('note-content').dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', ctrlKey: true, bubbles: true, cancelable: true}));

        await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    });

    it('should warn before leaving with an unsaved draft', () => {
        initCreateNotePage(document, {fetch: vi.fn()});

        document.getElementById('note-content').value = 'unsaved';
        document.getElementById('note-content').dispatchEvent(new Event('input', {bubbles: true}));
        const event = new Event('beforeunload', {cancelable: true});
        window.dispatchEvent(event);

        expect(event.defaultPrevented).toBe(true);
    });

    it('should reject invalid ticket responses before encrypting', async () => {
        const fetch = vi.fn(async () => Response.json({id: 'invalid', ticket: 'ticket', burnToken}, {status: 200}));

        initCreateNotePage(document, {fetch});
        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        await vi.waitFor(() => expect(document.getElementById('create-note-error-message').textContent).toBe('The server returned an invalid note ticket.'));
        expect(fetch).toHaveBeenCalledTimes(1);
    });

    it('should not attach duplicate submit handlers when initialized twice', async () => {
        const fetch = successfulCreateFetch();

        initCreateNotePage(document, {
            fetch,
            clipboard: {writeText: vi.fn()},
            crypto: fixedRandomCrypto(7),
        });
        initCreateNotePage(document, {
            fetch,
            clipboard: {writeText: vi.fn()},
            crypto: fixedRandomCrypto(7),
        });

        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        await vi.waitFor(() => expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(false));
        expect(fetch).toHaveBeenCalledTimes(2);
        expect(fetch).toHaveBeenNthCalledWith(1, '/api/tickets', expect.any(Object));
        expect(fetch).toHaveBeenNthCalledWith(2, `/api/notes/${noteId}`, expect.any(Object));
    });

    it('should show safe user-facing messages for server failures', async () => {
        const fetch = vi.fn(async url => {
            if (url === '/api/tickets') {
                return createTicketResponse();
            }
            return Response.json({error: 'ticket_unusable'}, {status: 400});
        });

        initCreateNotePage(document, {fetch, crypto: fixedRandomCrypto(3)});
        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        await vi.waitFor(() => expect(document.getElementById('create-note-error-message').textContent).toContain('ticket is no longer valid'));
        expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(true);
    });

    it.each([
        ['invalid JSON', new Response('not-json', {status: 201})],
        ['missing note ID', Response.json({}, {status: 201})],
        ['invalid note ID', Response.json({id: 'invalid'}, {status: 201})],
    ])('should reject %s success responses', async (_name, response) => {
        const fetch = vi.fn(async url => {
            if (url === '/api/tickets') {
                return createTicketResponse();
            }
            return response;
        });

        initCreateNotePage(document, {fetch, crypto: fixedRandomCrypto(3)});
        document.getElementById('note-content').value = 'Secret note';
        submitForm();

        await vi.waitFor(() => expect(document.getElementById('create-note-error-message').textContent).toBe('The server returned an invalid note link.'));
        expect(document.getElementById('create-note-form').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(true);
    });

    it('should show copy errors while the success panel is visible', async () => {
        vi.useFakeTimers();
        const clipboard = {writeText: vi.fn().mockRejectedValue(new Error('denied'))};

        initCreateNotePage(document, {
            fetch: successfulCreateFetch(),
            clipboard,
            crypto: fixedRandomCrypto(9),
        });
        document.getElementById('note-content').value = 'Secret note';
        submitForm();
        await vi.waitFor(() => expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(false));

        document.getElementById('copy-link-button').click();
        await vi.waitFor(() => expect(document.getElementById('create-note-error').classList.contains('hidden')).toBe(false));

        expect(document.getElementById('create-note-form').classList.contains('hidden')).toBe(true);
        expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('create-note-error-message').textContent).toBe('Could not copy the link.');
    });

    it('should share the created link when Web Share is available', async () => {
        const share = vi.fn().mockResolvedValue(undefined);

        initCreateNotePage(document, {
            fetch: successfulCreateFetch(),
            clipboard: {writeText: vi.fn()},
            crypto: fixedRandomCrypto(9),
            navigator: {share},
        });
        document.getElementById('note-content').value = 'Secret note';
        submitForm();
        await vi.waitFor(() => expect(document.getElementById('create-note-result').classList.contains('hidden')).toBe(false));

        expect(document.getElementById('share-link-button').classList.contains('hidden')).toBe(false);
        document.getElementById('share-link-button').click();

        await vi.waitFor(() => expect(share).toHaveBeenCalledWith(expect.objectContaining({
            title: 'One Time Note',
            url: expect.stringContaining(`/note/${noteId}#v1.`),
        })));
    });
});

function createPageHtml() {
    return `
        <form id="create-note-form">
            <input id="create-config" data-public-origin="https://notes.example.test" data-display-name="One Time Note" data-max-note-size="1048576">
            <textarea id="note-content"></textarea>
            <input id="expires-in-seconds" value="86400">
            <button type="button" id="expiration-toggle" aria-expanded="false"><span id="expiration-selected">1 day</span></button>
            <div id="expiration-menu" class="hidden">
                <button type="button" class="expiration-option" aria-selected="false" data-expires-in-seconds="3600">1 hour</button>
                <button type="button" class="expiration-option" aria-selected="false" data-expires-in-seconds="14400">4 hours</button>
                <button type="button" class="expiration-option" aria-selected="true" data-expires-in-seconds="86400">1 day</button>
                <button type="button" class="expiration-option" aria-selected="false" data-expires-in-seconds="259200">3 days</button>
                <button type="button" class="expiration-option" aria-selected="false" data-expires-in-seconds="604800">7 days</button>
            </div>
            <p id="note-size-status"></p>
            <button id="save-note-button"><span class="button-label">Save & Encrypt</span></button>
        </form>
        <section id="create-note-result" class="hidden">
            <p id="created-expiration-fact"></p>
            <input id="note-link">
            <button id="copy-link-button"><span class="button-label">Copy Link</span></button>
            <button id="share-link-button" class="hidden"><span class="button-label">Share Link</span></button>
        </section>
        <div id="create-note-error" class="hidden" tabindex="-1"><p id="create-note-error-message"></p></div>
        <span id="status-text">Encrypted in this browser</span>
    `;
}

function createTicketResponse() {
    return Response.json({id: noteId, ticket: 'fresh-ticket', burnToken, ticketExpiresAt: '2026-06-21T12:00:00Z'}, {status: 200});
}

function successfulCreateFetch(expiresInSeconds = 86400, onSave = () => {}) {
    return vi.fn(async (url, options) => {
        if (url === '/api/tickets') {
            return createTicketResponse();
        }
        expect(url).toBe(`/api/notes/${noteId}`);
        onSave(url, options);
        return Response.json({id: noteId, expiresInSeconds}, {status: 201});
    });
}

function submitForm() {
    document.getElementById('create-note-form').dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
}

function fixedRandomCrypto(byte) {
    return {
        subtle: globalThis.crypto.subtle,
        getRandomValues(bytes) {
            bytes.fill(byte);
            return bytes;
        },
    };
}

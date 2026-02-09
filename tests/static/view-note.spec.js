import {beforeEach, describe, expect, it, vi} from 'vitest';
import {initViewNotePage} from '../../web/static/view-note.js';
import {encode, encodeSecretFragment, encryptString} from '../../web/static/crypto-utils.js';

const noteId = 'C'.repeat(43);

describe('view-note page controller', () => {
    beforeEach(() => {
        vi.useRealTimers();
        document.body.innerHTML = viewPageHtml(noteId);
        window.history.replaceState(null, '', `/note/${noteId}`);
    });

    it('should retrieve, decrypt, display, and clear the URL key', async () => {
        const keyBytes = new Uint8Array(32).fill(4);
        const burnTokenBytes = new Uint8Array(32).fill(5);
        const payload = await encryptString('Recovered secret', keyBytes, noteId);
        window.history.replaceState(null, '', `/note/${noteId}#${encodeSecretFragment(keyBytes, burnTokenBytes)}`);

        const fetch = vi.fn().mockResolvedValue(Response.json({payload: JSON.stringify(payload)}, {status: 200}));

        initViewNotePage(document, {
            fetch,
            clipboard: {writeText: vi.fn()},
            history: window.history,
            location: window.location,
        });

        expect(window.location.hash).toBe('');

        document.getElementById('confirm-view-note').click();

        await vi.waitFor(() => expect(document.getElementById('decrypted-content').value).toBe('Recovered secret'));
        expect(fetch).toHaveBeenCalledWith(`/api/notes/${noteId}/open`, expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({burnToken: encode(burnTokenBytes)}),
            credentials: 'same-origin',
            cache: 'no-store',
            redirect: 'error',
            headers: {
                Accept: 'application/json',
                'Content-Type': 'application/json',
            },
        }));
        expect(document.getElementById('note-confirmation').classList.contains('hidden')).toBe(true);
        expect(document.getElementById('note-display').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('status-text').textContent).toBe('Decrypted in this browser');
        expect(document.activeElement).toBe(document.getElementById('decrypted-content'));
    });

    it('should show a retrieval error when the URL key is missing', () => {
        const fetch = vi.fn();

        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        document.getElementById('confirm-view-note').click();

        expect(fetch).not.toHaveBeenCalled();
        expect(document.getElementById('note-error').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('error-message').textContent).toBe('No note secret was found in the URL.');
        expect(document.activeElement).toBe(document.getElementById('note-error'));
    });

    it('should validate malformed URL keys before retrieving', () => {
        window.history.replaceState(null, '', `/note/${noteId}#abc`);
        const fetch = vi.fn();

        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        document.getElementById('confirm-view-note').click();

        expect(fetch).not.toHaveBeenCalled();
        expect(document.getElementById('error-message').textContent).toBe('This note link uses an unsupported secret format.');
        expect(window.location.hash).toBe('');
    });

    it('should not attach duplicate confirm handlers when initialized twice', async () => {
        const keyBytes = new Uint8Array(32).fill(4);
        const burnTokenBytes = new Uint8Array(32).fill(5);
        const payload = await encryptString('Recovered secret', keyBytes, noteId);
        window.history.replaceState(null, '', `/note/${noteId}#${encodeSecretFragment(keyBytes, burnTokenBytes)}`);
        const fetch = vi.fn(async () => Response.json({payload: JSON.stringify(payload)}, {status: 200}));

        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        document.getElementById('confirm-view-note').click();

        await vi.waitFor(() => expect(document.getElementById('decrypted-content').value).toBe('Recovered secret'));
        expect(fetch).toHaveBeenCalledTimes(1);
    });

    it('should map 404 responses to a safe user-facing message', async () => {
        const keyBytes = new Uint8Array(32).fill(4);
        const burnTokenBytes = new Uint8Array(32).fill(5);
        window.history.replaceState(null, '', `/note/${noteId}#${encodeSecretFragment(keyBytes, burnTokenBytes)}`);
        const fetch = vi.fn().mockResolvedValue(Response.json({error: 'note_open_failed'}, {status: 404}));

        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        document.getElementById('confirm-view-note').click();

        await vi.waitFor(() => expect(document.getElementById('error-message').textContent).toContain('could not be opened'));
        expect(document.getElementById('note-display').classList.contains('hidden')).toBe(true);
    });

    it('should map rate-limited opens to a clear user-facing message', async () => {
        const keyBytes = new Uint8Array(32).fill(4);
        const burnTokenBytes = new Uint8Array(32).fill(5);
        window.history.replaceState(null, '', `/note/${noteId}#${encodeSecretFragment(keyBytes, burnTokenBytes)}`);
        const fetch = vi.fn().mockResolvedValue(Response.json({error: 'rate_limited'}, {status: 429}));

        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        document.getElementById('confirm-view-note').click();

        await vi.waitFor(() => expect(document.getElementById('error-message').textContent).toBe('Too many failed attempts. Wait a moment and try again.'));
        expect(document.getElementById('note-display').classList.contains('hidden')).toBe(true);
    });

    it.each([
        ['invalid JSON', new Response('not-json', {status: 200})],
        ['missing payload', Response.json({}, {status: 200})],
        ['invalid payload', Response.json({payload: 'not valid!'}, {status: 200})],
        ['object payload', Response.json({payload: {protected: 'unsupported'}}, {status: 200})],
    ])('should reject %s success responses', async (_name, response) => {
        const keyBytes = new Uint8Array(32).fill(4);
        const burnTokenBytes = new Uint8Array(32).fill(5);
        window.history.replaceState(null, '', `/note/${noteId}#${encodeSecretFragment(keyBytes, burnTokenBytes)}`);
        const fetch = vi.fn().mockResolvedValue(response);

        initViewNotePage(document, {fetch, history: window.history, location: window.location});
        document.getElementById('confirm-view-note').click();

        await vi.waitFor(() => expect(document.getElementById('error-message').textContent).toBe('The server returned an invalid encrypted note.'));
        expect(document.getElementById('note-display').classList.contains('hidden')).toBe(true);
    });

    it('should show copy failures without hiding the decrypted note', async () => {
        const clipboard = {writeText: vi.fn().mockRejectedValue(new Error('denied'))};

        initViewNotePage(document, {clipboard});
        document.getElementById('decrypted-content').value = 'Recovered secret';
        document.getElementById('note-confirmation').classList.add('hidden');
        document.getElementById('note-display').classList.remove('hidden');

        document.getElementById('copy-note').click();

        await vi.waitFor(() => expect(document.getElementById('note-action-error').classList.contains('hidden')).toBe(false));
        expect(document.getElementById('note-display').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('note-error').classList.contains('hidden')).toBe(true);
        expect(document.getElementById('note-action-error-message').textContent).toBe('Could not copy the note.');
        expect(document.activeElement).toBe(document.getElementById('note-action-error'));
    });

    it('should download the decrypted note as text', () => {
        const revokeObjectURL = vi.fn();
        const URL = {
            createObjectURL: vi.fn(() => 'blob:note'),
            revokeObjectURL,
        };
        const clicks = [];
        const documentRef = {
            createElement: vi.fn(() => ({
                click: vi.fn(function () {
                    clicks.push({href: this.href, download: this.download});
                }),
            })),
        };

        initViewNotePage(document, {URL, document: documentRef});
        document.getElementById('decrypted-content').value = 'Recovered secret';
        document.getElementById('download-note').click();

        expect(URL.createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
        expect(clicks).toEqual([{href: 'blob:note', download: 'one-time-note.txt'}]);
        expect(revokeObjectURL).toHaveBeenCalledWith('blob:note');
    });
});

function viewPageHtml(id) {
    return `
        <div id="note-confirmation">
            <button id="confirm-view-note" data-id="${id}"><span class="button-label">View Note</span></button>
        </div>
        <div id="note-display" class="hidden">
            <textarea id="decrypted-content"></textarea>
            <button id="copy-note"><span class="button-label">Copy Note</span></button>
            <button id="download-note"><span class="button-label">Download .txt</span></button>
        </div>
        <div id="note-error" class="hidden" tabindex="-1"><p id="error-message"></p></div>
        <div id="note-action-error" class="hidden" tabindex="-1"><p id="note-action-error-message"></p></div>
        <span id="status-text">One-time note</span>
    `;
}

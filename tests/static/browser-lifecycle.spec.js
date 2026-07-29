import {describe, expect, it, vi} from 'vitest';
import {initBrowserLifecycle} from '../../web/static/browser-lifecycle.js';

describe('browser lifecycle', () => {
    it.each([
        ['#v1.note-secret', true],
        ['#ordinary-anchor', false],
        ['', false],
    ])('handles non-view fragment %s', (hash, shouldScrub) => {
        const history = {replaceState: vi.fn()};
        const location = {hash, pathname: '/missing', search: '?from=test'};

        initBrowserLifecycle(document, {
            history,
            location,
            window: new EventTarget(),
        });

        if (shouldScrub) {
            expect(history.replaceState).toHaveBeenCalledWith(null, '', '/missing?from=test');
        } else {
            expect(history.replaceState).not.toHaveBeenCalled();
        }
    });

    it('preserves a note secret on the view page', () => {
        document.body.innerHTML = '<div data-page="view-note"></div>';
        const history = {replaceState: vi.fn()};
        const location = {
            get hash() {
                throw new Error('view-page fragment was read');
            },
            pathname: '/note/id',
            search: '',
        };

        initBrowserLifecycle(document, {
            history,
            location,
            window: new EventTarget(),
        });

        expect(history.replaceState).not.toHaveBeenCalled();
    });

    it('clears sensitive DOM fields on pagehide', () => {
        document.body.innerHTML = `
            <textarea id="note-content">unsaved note</textarea>
            <input id="note-link" value="https://notes.test/note/id#v1.secret">
            <textarea id="decrypted-content">decrypted note</textarea>
        `;
        const windowRef = new EventTarget();
        initBrowserLifecycle(document, {
            history: {replaceState: vi.fn()},
            location: {hash: '', pathname: '/', search: ''},
            window: windowRef,
        });

        windowRef.dispatchEvent(new Event('pagehide'));

        expect(document.getElementById('note-content').value).toBe('');
        expect(document.getElementById('note-link').value).toBe('');
        expect(document.getElementById('decrypted-content').value).toBe('');
    });

    it.each([
        [true, 1],
        [false, 0],
    ])('reloads pageshow when persisted is %s', (persisted, reloadCount) => {
        const windowRef = new EventTarget();
        const location = {hash: '', pathname: '/', search: '', reload: vi.fn()};
        initBrowserLifecycle(document, {
            history: {replaceState: vi.fn()},
            location,
            window: windowRef,
        });
        const event = new Event('pageshow');
        Object.defineProperty(event, 'persisted', {value: persisted});

        windowRef.dispatchEvent(event);

        expect(location.reload).toHaveBeenCalledTimes(reloadCount);
    });
});

import {describe, expect, it, vi} from 'vitest';
import {
    buttonLabel,
    clearError,
    copyText,
    displayMessageFor,
    hide,
    requiredElement,
    setButtonBusy,
    setError,
    show,
    showTemporaryButtonLabel
} from '../../web/static/dom.js';
import {UserVisibleError} from '../../web/static/crypto-utils.js';

describe('dom helpers', () => {
    it('requiredElement should return an element or fail loudly', () => {
        document.body.innerHTML = '<div id="target"></div>';

        expect(requiredElement('target')).toBeInstanceOf(HTMLDivElement);
        expect(() => requiredElement('missing')).toThrow('Missing required element #missing.');
    });

    it('setButtonBusy should update and restore button state', () => {
        document.body.innerHTML = '<button><span class="button-label">Save</span></button>';
        const button = document.querySelector('button');

        const restore = setButtonBusy(button, 'Saving...');

        expect(button.disabled).toBe(true);
        expect(buttonLabel(button).textContent).toBe('Saving...');

        restore();

        expect(button.disabled).toBe(false);
        expect(buttonLabel(button).textContent).toBe('Save');
    });

    it('show, hide, setError, and clearError should manage visible error state', () => {
        document.body.innerHTML = '<div id="panel" class="hidden" tabindex="-1"><p id="message"></p></div>';
        const panel = requiredElement('panel');
        const message = requiredElement('message');

        show(panel);
        expect(panel.classList.contains('hidden')).toBe(false);

        hide(panel);
        expect(panel.classList.contains('hidden')).toBe(true);

        setError(panel, message, 'Failed');
        expect(panel.classList.contains('hidden')).toBe(false);
        expect(message.textContent).toBe('Failed');

        setError(panel, message, 'Failed again', {focus: true});
        expect(document.activeElement).toBe(panel);

        clearError(panel, message);
        expect(panel.classList.contains('hidden')).toBe(true);
        expect(message.textContent).toBe('');
    });

    it('showTemporaryButtonLabel should reset overlapping restore timers', () => {
        vi.useFakeTimers();
        try {
            document.body.innerHTML = '<button><span class="button-label">Copy</span></button>';
            const button = document.querySelector('button');

            showTemporaryButtonLabel(button, 'Copied');
            vi.advanceTimersByTime(1000);
            showTemporaryButtonLabel(button, 'Copied');
            vi.advanceTimersByTime(1000);

            expect(buttonLabel(button).textContent).toBe('Copied');

            vi.advanceTimersByTime(1000);
            expect(buttonLabel(button).textContent).toBe('Copy');
        } finally {
            vi.useRealTimers();
        }
    });

    it('displayMessageFor should only expose user-visible error details', () => {
        expect(displayMessageFor(new UserVisibleError('Show this'), 'Fallback')).toBe('Show this');
        expect(displayMessageFor(new Error('Internal detail'), 'Fallback')).toBe('Fallback');
    });

    it('copyText should use clipboard APIs and report unavailable clipboard access', async () => {
        const clipboard = {writeText: vi.fn().mockResolvedValue(undefined)};

        await copyText('secret', clipboard);

        expect(clipboard.writeText).toHaveBeenCalledWith('secret');
        await expect(copyText('', clipboard)).rejects.toThrow(UserVisibleError);
        await expect(copyText('secret', {})).rejects.toThrow(UserVisibleError);
    });
});

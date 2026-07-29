import {copyText, clearError, displayMessageFor, focusElement, requiredElement, setButtonBusy, setError, show, hide, showTemporaryButtonLabel} from "./dom.js";
import {clearAnchor, decryptString, getSecretFragmentFromAnchor, parseSecretFragment, UserVisibleError} from "./crypto-utils.js";

const NOTE_ID_PATTERN = /^[A-Za-z0-9_-]{43}$/;

class RetryableOpenError extends UserVisibleError {}

export function initViewNotePage(root = document, options = {}) {
    const deps = {
        fetch: options.fetch ?? globalThis.fetch?.bind(globalThis),
        clipboard: options.clipboard ?? navigator.clipboard,
        history: options.history ?? window.history,
        location: options.location ?? window.location,
        crypto: options.crypto ?? globalThis.crypto,
        URL: options.URL ?? globalThis.URL,
        document: options.document ?? document,
        window: options.window ?? window,
    };

    const confirmation = requiredElement("note-confirmation", root);
    if (confirmation.dataset.jsInitialized === "true") {
        return;
    }
    confirmation.dataset.jsInitialized = "true";

    const display = requiredElement("note-display", root);
    const errorPanel = requiredElement("note-error", root);
    const errorMessage = requiredElement("error-message", root);
    const actionErrorPanel = requiredElement("note-action-error", root);
    const actionErrorMessage = requiredElement("note-action-error-message", root);
    const confirmButton = requiredElement("confirm-view-note", root);
    const cancelLink = requiredElement("cancel-view-note", root);
    const decryptedContent = requiredElement("decrypted-content", root);
    const copyButton = requiredElement("copy-note", root);
    const downloadButton = requiredElement("download-note", root);
    const statusText = requiredElement("status-text", root);

    const openState = {
        started: false,
        secret: null,
        secretError: null,
    };
    let pageActive = true;

    deps.window?.addEventListener?.("pagehide", () => {
        pageActive = false;
    });

    confirmButton.addEventListener("click", () => {
        beginOpening(openState, deps);
        void retrieveNote({
            deps,
            openState,
            confirmation,
            display,
            errorPanel,
            errorMessage,
            actionErrorPanel,
            actionErrorMessage,
            confirmButton,
            decryptedContent,
            statusText,
            pageActive: () => pageActive,
        });
    });
    cancelLink.addEventListener("click", () => {
        clearAnchor(deps.history, deps.location);
    });

    copyButton.addEventListener("click", () => {
        void copyDecryptedNote(copyButton, decryptedContent, actionErrorPanel, actionErrorMessage, deps.clipboard);
    });
    downloadButton.addEventListener("click", () => {
        downloadDecryptedNote(decryptedContent, actionErrorPanel, actionErrorMessage, deps);
    });
}

function beginOpening(openState, deps) {
    if (openState.started) {
        return;
    }
    openState.started = true;

    const fragment = getSecretFragmentFromAnchor(deps.location);
    try {
        if (fragment) {
            openState.secret = parseSecretFragment(fragment);
        }
    } catch (error) {
        openState.secretError = error;
    } finally {
        clearAnchor(deps.history, deps.location);
    }
}

async function retrieveNote(ui) {
    clearError(ui.actionErrorPanel, ui.actionErrorMessage);

    const noteId = ui.confirmButton.dataset.id;
    if (!NOTE_ID_PATTERN.test(noteId)) {
        showNoteError(ui, "This note link is invalid.");
        return;
    }

    if (ui.openState.secretError) {
        showNoteError(ui, displayMessageFor(ui.openState.secretError, "The note secret in this URL is invalid."));
        return;
    }
    if (!ui.openState.secret) {
        showNoteError(ui, "No note secret was found in the URL.");
        return;
    }

    if (!ui.deps.fetch) {
        showNoteError(ui, "Network requests are unavailable in this browser.");
        return;
    }

    const restoreButton = setButtonBusy(ui.confirmButton, "Retrieving...");

    try {
        let response;
        try {
            response = await ui.deps.fetch(`/api/notes/${noteId}/open`, {
                method: "POST",
                body: JSON.stringify({burnToken: ui.openState.secret.burnToken}),
                credentials: "same-origin",
                cache: "no-store",
                redirect: "error",
                headers: {
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                },
            });
        } catch {
            throw new RetryableOpenError("Could not open this note.");
        }

        if (!response.ok) {
            const message = retrieveErrorMessage(response);
            if (response.status === 429 || response.status >= 500) {
                throw new RetryableOpenError(message);
            }
            throw new UserVisibleError(message);
        }

        const payload = await readOpenNotePayload(response);
        try {
            const plaintext = await decryptString(payload, ui.openState.secret.keyBytes, noteId, ui.deps.crypto);
            if (!ui.pageActive()) {
                return;
            }
            ui.decryptedContent.value = plaintext;
        } catch {
            throw new UserVisibleError("This note was retrieved but could not be decrypted. It may already be consumed.");
        }
        hide(ui.confirmation);
        hide(ui.errorPanel);
        hide(ui.actionErrorPanel);
        show(ui.display);
        focusElement(ui.decryptedContent);
        ui.statusText.textContent = "Decrypted in this browser";
    } catch (error) {
        const message = displayMessageFor(error, "Could not open this note.");
        if (error instanceof RetryableOpenError) {
            showRetryableError(ui, message);
        } else {
            showNoteError(ui, message);
        }
    } finally {
        restoreButton();
    }
}

async function copyDecryptedNote(button, decryptedContent, errorPanel, errorMessage, clipboard) {
    try {
        clearError(errorPanel, errorMessage);
        await copyText(decryptedContent.value, clipboard);
        showTemporaryButtonLabel(button, "Copied");
    } catch (error) {
        setError(errorPanel, errorMessage, displayMessageFor(error, "Could not copy the note."), {focus: true});
    }
}

function downloadDecryptedNote(decryptedContent, errorPanel, errorMessage, deps) {
    try {
        clearError(errorPanel, errorMessage);
        if (!decryptedContent.value) {
            throw new UserVisibleError("There is nothing to download yet.");
        }
        if (!deps.URL?.createObjectURL || !deps.document?.createElement) {
            throw new UserVisibleError("Downloads are unavailable in this browser.");
        }

        const blob = new Blob([decryptedContent.value], {type: "text/plain;charset=utf-8"});
        const url = deps.URL.createObjectURL(blob);
        try {
            const link = deps.document.createElement("a");
            link.href = url;
            link.download = "one-time-note.txt";
            link.rel = "noopener";
            link.click();
        } finally {
            deps.URL.revokeObjectURL?.(url);
        }
    } catch (error) {
        setError(errorPanel, errorMessage, displayMessageFor(error, "Could not download the note."), {focus: true});
    }
}

function showRetryableError(ui, message) {
    hide(ui.display);
    hide(ui.errorPanel);
    show(ui.confirmation);
    setError(ui.actionErrorPanel, ui.actionErrorMessage, message, {focus: true});
}

function showNoteError(ui, message) {
    hide(ui.confirmation);
    hide(ui.display);
    hide(ui.actionErrorPanel);
    setError(ui.errorPanel, ui.errorMessage, message, {focus: true});
}

function retrieveErrorMessage(response) {
    if (response.status === 404) {
        return "This note could not be opened. It may be missing, expired, already opened, or the link may be incomplete.";
    }
    if (response.status === 400) {
        return "This note link is invalid.";
    }
    if (response.status === 429) {
        return "Too many failed attempts. Wait a moment and try again.";
    }
    return "Could not retrieve this note. Try again.";
}

async function readOpenNotePayload(response) {
    let responseBody;
    try {
        responseBody = await response.json();
    } catch {
        throw new UserVisibleError("The server returned an invalid encrypted note.");
    }

    if (!responseBody?.payload || typeof responseBody.payload !== "string") {
        throw new UserVisibleError("The server returned an invalid encrypted note.");
    }
    try {
        return JSON.parse(responseBody.payload);
    } catch {
        throw new UserVisibleError("The server returned an invalid encrypted note.");
    }
}

if (document.querySelector('[data-page="view-note"]')) {
    initViewNotePage();
}

import {copyText, clearError, displayMessageFor, focusElement, requiredElement, setButtonBusy, setError, show, hide, buttonLabel, showTemporaryButtonLabel} from "./dom.js";
import {decodeBurnToken, encodeSecretFragment, encryptString, generateKeyBytes, UserVisibleError} from "./crypto-utils.js";

const NOTE_ID_PATTERN = /^[A-Za-z0-9_-]{43}$/;
const GCM_IV_BYTES = 12;
const GCM_TAG_BYTES = 16;
const ENCRYPTED_PAYLOAD_PROTECTED_HEADER_BYTES = 35;
const SecondsPerMinute = 60;
const SecondsPerHour = 60 * 60;
const SecondsPerDay = 24 * SecondsPerHour;

export function initCreateNotePage(root = document, options = {}) {
    const deps = {
        fetch: options.fetch ?? globalThis.fetch?.bind(globalThis),
        clipboard: options.clipboard ?? navigator.clipboard,
        location: options.location ?? window.location,
        crypto: options.crypto ?? globalThis.crypto,
        navigator: options.navigator ?? navigator,
        window: options.window ?? window,
    };

    const form = requiredElement("create-note-form", root);
    if (form.dataset.jsInitialized === "true") {
        return;
    }
    form.dataset.jsInitialized = "true";

    const config = requiredElement("create-config", root);
    const content = requiredElement("note-content", root);
    const submitButton = requiredElement("save-note-button", root);
    const result = requiredElement("create-note-result", root);
    const noteLink = requiredElement("note-link", root);
    const copyButton = requiredElement("copy-link-button", root);
    const shareButton = requiredElement("share-link-button", root);
    const statusText = requiredElement("status-text", root);
    const errorPanel = requiredElement("create-note-error", root);
    const errorMessage = requiredElement("create-note-error-message", root);
    const expirationValue = requiredElement("expires-in-seconds", root);
    const expirationOptionsRoot = requiredElement("expiration-options", root);
    const customExpirationToggle = requiredElement("custom-expiration-toggle", root);
    const customExpiration = requiredElement("custom-expiration", root);
    const customExpirationAmount = requiredElement("custom-expiration-amount", root);
    const customExpirationDecrement = requiredElement("custom-expiration-decrement", root);
    const customExpirationIncrement = requiredElement("custom-expiration-increment", root);
    const customExpirationUnit = requiredElement("custom-expiration-unit", root);
    const customExpirationStatus = requiredElement("custom-expiration-status", root);
    const sizeStatus = requiredElement("note-size-status", root);
    const expirationFact = requiredElement("created-expiration-fact", root);
    const expirationOptions = Array.from(expirationOptionsRoot.querySelectorAll("[data-expires-in-seconds]"));

    const limits = readLimits(config, expirationOptions);
    const encoder = new TextEncoder();
    let hasUnsavedDraft = false;
    let pageActive = true;

    submitButton.type = "submit";
    focusElement(content);
    updateSizeStatus(content, sizeStatus, submitButton, limits.maxNoteSize, encoder);
    if (typeof deps.navigator?.share === "function") {
        show(shareButton);
    }

    form.addEventListener("submit", ev => {
        ev.preventDefault();
        void createNote({
            deps,
            form,
            limits,
            config,
            content,
            submitButton,
            result,
            noteLink,
            expirationValue,
            expirationFact,
            statusText,
            errorPanel,
            errorMessage,
            encoder,
            markSaved: () => {
                hasUnsavedDraft = false;
            },
            pageActive: () => pageActive,
        });
    });

    content.addEventListener("input", () => {
        hasUnsavedDraft = content.value.length > 0;
        updateSizeStatus(content, sizeStatus, submitButton, limits.maxNoteSize, encoder);
    });
    content.addEventListener("keydown", ev => {
        if ((ev.ctrlKey || ev.metaKey) && ev.key === "Enter") {
            ev.preventDefault();
            form.requestSubmit();
        }
    });
    expirationOptionsRoot.addEventListener("click", ev => {
        const option = ev.target.closest("[data-expires-in-seconds]");
        if (!option) {
            return;
        }
        selectExpirationOption(option, expirationOptions, customExpirationToggle, customExpiration, expirationValue);
        clearError(errorPanel, errorMessage);
    });
    expirationOptionsRoot.addEventListener("keydown", ev => {
        if (ev.key === "ArrowRight" || ev.key === "ArrowDown" || ev.key === "ArrowLeft" || ev.key === "ArrowUp") {
            ev.preventDefault();
            const choices = [...expirationOptions, customExpirationToggle];
            const direction = ev.key === "ArrowRight" || ev.key === "ArrowDown" ? 1 : -1;
            focusAdjacentExpirationOption(choices, direction);
        }
    });
    const refreshCustomExpiration = () => {
        updateCustomExpiration(
            customExpirationAmount,
            customExpirationUnit,
            customExpirationStatus,
            expirationOptions,
            customExpirationToggle,
            expirationValue,
            limits,
        );
        updateStepperButtons(customExpirationAmount, customExpirationDecrement, customExpirationIncrement);
    };
    customExpirationToggle.addEventListener("click", () => {
        selectCustomExpiration(
            expirationOptions,
            customExpirationToggle,
            customExpiration,
            customExpirationAmount,
            customExpirationUnit,
            expirationValue,
        );
        refreshCustomExpiration();
        clearError(errorPanel, errorMessage);
        focusElement(customExpirationAmount);
    });
    customExpirationAmount.addEventListener("input", refreshCustomExpiration);
    customExpirationUnit.addEventListener("change", refreshCustomExpiration);
    customExpirationDecrement.addEventListener("click", () => {
        stepCustomAmount(customExpirationAmount, -1);
        refreshCustomExpiration();
    });
    customExpirationIncrement.addEventListener("click", () => {
        stepCustomAmount(customExpirationAmount, 1);
        refreshCustomExpiration();
    });
    deps.window?.addEventListener?.("pagehide", () => {
        pageActive = false;
        hasUnsavedDraft = false;
    });
    deps.window?.addEventListener?.("beforeunload", ev => {
        if (!hasUnsavedDraft) {
            return;
        }
        ev.preventDefault();
    });
    noteLink.addEventListener("click", () => noteLink.select());

    copyButton.addEventListener("click", () => {
        void copyResultLink(copyButton, noteLink, errorPanel, errorMessage, deps.clipboard);
    });
    shareButton.addEventListener("click", () => {
        void shareResultLink(shareButton, noteLink, config.dataset.displayName, errorPanel, errorMessage, deps.navigator);
    });
}

async function createNote(ui) {
    clearError(ui.errorPanel, ui.errorMessage);

    if (!ui.deps.fetch) {
        showCreateError(ui, "Network requests are unavailable in this browser.");
        return;
    }

    const plaintext = ui.content.value;
    if (!plaintext) {
        showCreateError(ui, "Enter a note before encrypting.");
        return;
    }
    if (estimatedPayloadBytes(plaintext, ui.encoder) > ui.limits.maxNoteSize) {
        showCreateError(ui, "This note is too large to save.");
        return;
    }

    let expiresInSeconds;
    try {
        expiresInSeconds = readExpiration(ui.expirationValue, ui.limits);
    } catch (error) {
        showCreateError(ui, displayMessageFor(error, expirationRangeMessage(ui.limits)));
        return;
    }

    const restoreButton = setButtonBusy(ui.submitButton, "Preparing...");

    try {
        const noteTicket = await requestCreateTicket(ui.deps.fetch);
        const noteId = noteTicket.id;
        const burnTokenBytes = noteTicket.burnTokenBytes;

        buttonLabel(ui.submitButton).textContent = "Encrypting...";
        const keyBytes = generateKeyBytes(ui.deps.crypto);
        const encryptedPayload = await encryptString(plaintext, keyBytes, noteId, ui.deps.crypto);
        const payload = JSON.stringify(encryptedPayload);
        const body = JSON.stringify({
            ticket: noteTicket.ticket,
            burnToken: noteTicket.burnToken,
            payload,
            expiresInSeconds,
        });

        buttonLabel(ui.submitButton).textContent = "Saving...";
        const response = await ui.deps.fetch(`/api/notes/${noteId}`, {
            method: "POST",
            body,
            credentials: "same-origin",
            cache: "no-store",
            redirect: "error",
            headers: {
                "Accept": "application/json",
                "Content-Type": "application/json",
            },
        });

        if (!response.ok) {
            throw new UserVisibleError(await saveErrorMessage(response, ui.limits));
        }

        const savedNote = await readCreateNoteResponse(response);
        if (savedNote.id !== noteId) {
            throw new UserVisibleError("The server returned an invalid note link.");
        }
        if (!ui.pageActive()) {
            return;
        }

        const publicOrigin = ui.config.dataset.publicOrigin || ui.deps.location.origin;
        ui.noteLink.value = buildNoteUrl(publicOrigin, savedNote.id, keyBytes, burnTokenBytes);
        ui.expirationFact.textContent = `Expires after ${formatDuration(savedNote.expiresInSeconds ?? expiresInSeconds)}`;
        ui.content.value = "";
        ui.markSaved();
        hide(ui.form);
        show(ui.result);
        focusElement(ui.noteLink);
        ui.noteLink.select();
        ui.statusText.textContent = "Secret stored only in this URL";
    } catch (error) {
        showCreateError(ui, displayMessageFor(error, "Could not create the note. Try again."));
    } finally {
        restoreButton();
    }
}

async function requestCreateTicket(fetchRef) {
    const response = await fetchRef("/api/tickets", {
        method: "POST",
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        headers: {
            "Accept": "application/json",
        },
    });

    if (!response.ok) {
        throw new UserVisibleError(await ticketErrorMessage(response));
    }

    return readCreateTicketResponse(response);
}

async function copyResultLink(button, noteLink, errorPanel, errorMessage, clipboard) {
    clearError(errorPanel, errorMessage);
    try {
        await copyText(noteLink.value, clipboard);
        showTemporaryButtonLabel(button, "Copied");
    } catch (error) {
        setError(errorPanel, errorMessage, displayMessageFor(error, "Could not copy the link."), {focus: true});
    }
}

async function shareResultLink(button, noteLink, displayName, errorPanel, errorMessage, navigatorRef) {
    clearError(errorPanel, errorMessage);
    if (!navigatorRef?.share) {
        setError(errorPanel, errorMessage, "Sharing is unavailable in this browser.", {focus: true});
        return;
    }
    try {
        await navigatorRef.share({
            title: displayName || "One Time Note",
            text: "Open this encrypted note once.",
            url: noteLink.value,
        });
        showTemporaryButtonLabel(button, "Shared");
    } catch (error) {
        if (error?.name === "AbortError") {
            return;
        }
        setError(errorPanel, errorMessage, displayMessageFor(error, "Could not share the link."), {focus: true});
    }
}

function showCreateError(ui, message) {
    setError(ui.errorPanel, ui.errorMessage, message, {focus: true});
}

function buildNoteUrl(origin, noteId, keyBytes, burnTokenBytes) {
    const url = new URL(origin);
    url.pathname = `/note/${noteId}`;
    url.hash = encodeSecretFragment(keyBytes, burnTokenBytes);
    return url.toString();
}

async function saveErrorMessage(response, limits) {
    let code = "";
    try {
        code = (await response.json()).error;
    } catch {
        code = "";
    }
    if (code === "invalid_expiration") {
        return expirationRangeMessage(limits);
    }
    if (code === "ticket_unusable" || response.status === 400 || response.status === 409) {
        return "This note ticket is no longer valid. Try again.";
    }
    if (code === "note_too_large" || response.status === 413) {
        return "This note is too large to save.";
    }
    if (code === "storage_full" || response.status === 507) {
        return "The service is temporarily full. Try again later.";
    }
    return "Could not save this note. Try again.";
}

async function ticketErrorMessage(response) {
    if (response.status === 429) {
        return "Too many requests. Wait a moment and try again.";
    }
    return "Could not prepare this note. Try again.";
}

async function readCreateTicketResponse(response) {
    let responseBody;
    try {
        responseBody = await response.json();
    } catch {
        throw new UserVisibleError("The server returned an invalid note ticket.");
    }

    const id = responseBody?.id;
    const ticket = responseBody?.ticket;
    const burnToken = responseBody?.burnToken;
    if (!NOTE_ID_PATTERN.test(id) || typeof ticket !== "string" || ticket === "" || typeof burnToken !== "string") {
        throw new UserVisibleError("The server returned an invalid note ticket.");
    }

    let burnTokenBytes;
    try {
        burnTokenBytes = decodeBurnToken(burnToken);
    } catch {
        throw new UserVisibleError("The server returned an invalid note ticket.");
    }

    return {id, ticket, burnToken, burnTokenBytes};
}

async function readCreateNoteResponse(response) {
    let responseBody;
    try {
        responseBody = await response.json();
    } catch {
        throw new UserVisibleError("The server returned an invalid note link.");
    }

    const id = responseBody?.id;
    if (!NOTE_ID_PATTERN.test(id)) {
        throw new UserVisibleError("The server returned an invalid note link.");
    }
    return {
        id,
        expiresInSeconds: Number.isFinite(responseBody?.expiresInSeconds) ? responseBody.expiresInSeconds : null,
    };
}

function readLimits(config, expirationOptions) {
    const expirationLimits = readExpirationLimits(expirationOptions);
    return {
        maxNoteSize: readPositiveInt(config.dataset.maxNoteSize, 1024 * 1024),
        ...expirationLimits,
    };
}

function readExpirationLimits(expirationOptions) {
    const values = expirationOptions
        .map(option => Number(option.dataset.expiresInSeconds))
        .filter(seconds => Number.isInteger(seconds) && seconds > 0);
    if (values.length === 0) {
        return {
            minExpiresInSeconds: SecondsPerHour,
            maxExpiresInSeconds: 7 * SecondsPerDay,
        };
    }
    return {
        minExpiresInSeconds: Math.min(...values),
        maxExpiresInSeconds: Math.max(...values),
    };
}

function readPositiveInt(value, fallback) {
    const parsed = Number.parseInt(value, 10);
    if (!Number.isFinite(parsed) || parsed <= 0) {
        return fallback;
    }
    return parsed;
}

function readExpiration(expirationValue, limits) {
    const seconds = Number(expirationValue.value);
    if (!Number.isInteger(seconds) || seconds <= 0) {
        throw new UserVisibleError(expirationRangeMessage(limits));
    }
    if (seconds < limits.minExpiresInSeconds || seconds > limits.maxExpiresInSeconds) {
        throw new UserVisibleError(expirationRangeMessage(limits));
    }
    return seconds;
}

function updateSizeStatus(content, status, submitButton, maxNoteSize, encoder) {
    const textBytes = encoder.encode(content.value).length;
    const estimated = estimatedPayloadBytes(content.value, encoder);
    status.textContent = textBytes === 0
        ? `Limit ${formatBytes(maxNoteSize)}`
        : `${formatBytes(textBytes)} of ${formatBytes(maxNoteSize)}`;
    const tooLarge = estimated > maxNoteSize;
    status.classList.toggle("helper-text-danger", tooLarge);
    submitButton.disabled = tooLarge;
}

function estimatedPayloadBytes(text, encoder) {
    const textBytes = encoder.encode(text).length;
    const envelope = {
        protected: "A".repeat(base64urlEncodedLength(ENCRYPTED_PAYLOAD_PROTECTED_HEADER_BYTES)),
        iv: "A".repeat(base64urlEncodedLength(GCM_IV_BYTES)),
        ct: "A".repeat(base64urlEncodedLength(textBytes + GCM_TAG_BYTES)),
    };
    return JSON.stringify(envelope).length;
}

function base64urlEncodedLength(byteLength) {
    if (byteLength === 0) {
        return 0;
    }
    const padding = (3 - (byteLength % 3)) % 3;
    return 4 * Math.ceil(byteLength / 3) - padding;
}

function formatBytes(bytes) {
    if (bytes >= 1024 * 1024) {
        return `${formatNumber(bytes / (1024 * 1024))} MiB`;
    }
    if (bytes >= 1024) {
        return `${formatNumber(bytes / 1024)} KiB`;
    }
    return `${bytes} bytes`;
}

function formatNumber(value) {
    if (Number.isInteger(value)) {
        return value.toString();
    }
    return value.toFixed(1);
}

function formatDuration(seconds) {
    if (seconds % SecondsPerDay === 0) {
        const days = seconds / SecondsPerDay;
        return `${days} ${days === 1 ? "day" : "days"}`;
    }
    if (seconds % SecondsPerHour === 0) {
        const hours = seconds / SecondsPerHour;
        return `${hours} ${hours === 1 ? "hour" : "hours"}`;
    }
    if (seconds % SecondsPerMinute === 0) {
        const minutes = seconds / SecondsPerMinute;
        return `${minutes} ${minutes === 1 ? "minute" : "minutes"}`;
    }
    return `${seconds} ${seconds === 1 ? "second" : "seconds"}`;
}

function expirationRangeMessage(limits) {
    return `Choose an expiration between ${formatDuration(limits.minExpiresInSeconds)} and ${formatDuration(limits.maxExpiresInSeconds)}.`;
}

function selectExpirationOption(option, options, customToggle, customExpiration, expirationValue) {
    expirationValue.value = option.dataset.expiresInSeconds;
    for (const item of options) {
        item.setAttribute("aria-selected", item === option ? "true" : "false");
    }
    customToggle.setAttribute("aria-selected", "false");
    customToggle.setAttribute("aria-expanded", "false");
    setCustomExpirationEnabled(customExpiration, false);
    hide(customExpiration);
}

function selectCustomExpiration(options, customToggle, customExpiration, customAmount, customUnit, expirationValue) {
    for (const item of options) {
        item.setAttribute("aria-selected", "false");
    }
    customToggle.setAttribute("aria-selected", "true");
    customToggle.setAttribute("aria-expanded", "true");
    setCustomExpirationEnabled(customExpiration, true);
    show(customExpiration);

    const seconds = Number(expirationValue.value);
    const unit = seconds % SecondsPerDay === 0 ? SecondsPerDay : SecondsPerHour;
    setCustomUnit(customUnit, unit);
    customAmount.value = String(seconds / unit);
}

function selectedCustomUnit(customUnit) {
    return Number(customUnit.querySelector("input:checked")?.value);
}

function setCustomUnit(customUnit, unit) {
    for (const radio of customUnit.querySelectorAll("input[type=radio]")) {
        radio.checked = Number(radio.value) === unit;
    }
}

function stepCustomAmount(customAmount, delta) {
    const min = Number(customAmount.min);
    const max = Number(customAmount.max);
    const current = Number(customAmount.value);
    const next = Number.isInteger(current) && customAmount.value !== "" ? current + delta : min;
    customAmount.value = String(Math.min(max, Math.max(min, next)));
}

function updateStepperButtons(customAmount, decrement, increment) {
    const amount = Number(customAmount.value);
    decrement.disabled = customAmount.disabled || amount <= Number(customAmount.min);
    increment.disabled = customAmount.disabled || amount >= Number(customAmount.max);
}

function setCustomExpirationEnabled(customExpiration, enabled) {
    for (const control of customExpiration.querySelectorAll("input, button")) {
        control.disabled = !enabled;
    }
}

function focusAdjacentExpirationOption(options, direction) {
    const currentIndex = options.indexOf(document.activeElement);
    const nextIndex = currentIndex < 0 ? 0 : (currentIndex + direction + options.length) % options.length;
    focusElement(options[nextIndex]);
}

function updateCustomExpiration(customAmount, customUnit, status, options, customToggle, expirationValue, limits) {
    const amount = Number(customAmount.value);
    const unit = selectedCustomUnit(customUnit);
    const seconds = amount * unit;
    customAmount.min = String(Math.ceil(limits.minExpiresInSeconds / unit));
    customAmount.max = String(Math.floor(limits.maxExpiresInSeconds / unit));
    const valid = Number.isInteger(amount) &&
        customAmount.validity.valid &&
        Number.isInteger(seconds) &&
        seconds >= limits.minExpiresInSeconds &&
        seconds <= limits.maxExpiresInSeconds;

    expirationValue.value = valid ? String(seconds) : "";
    status.textContent = valid ? `Custom expiration: ${formatDuration(seconds)}` : expirationRangeMessage(limits);
    for (const item of options) {
        item.setAttribute("aria-selected", "false");
    }
    customToggle.setAttribute("aria-selected", "true");
}

if (document.querySelector('[data-page="create-note"]')) {
    initCreateNotePage();
}

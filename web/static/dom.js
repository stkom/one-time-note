import {UserVisibleError} from "./crypto-utils.js";

const TemporaryButtonLabels = new WeakMap();

export function requiredElement(id, root = document) {
    const element = root.getElementById(id);
    if (!element) {
        throw new Error(`Missing required element #${id}.`);
    }
    return element;
}

export function buttonLabel(button) {
    return button.querySelector(".button-label") ?? button;
}

export function setButtonBusy(button, labelText) {
    const label = buttonLabel(button);
    const previousText = label.textContent;
    button.disabled = true;
    label.textContent = labelText;

    return () => {
        button.disabled = false;
        label.textContent = previousText;
    };
}

export function showTemporaryButtonLabel(button, labelText, durationMs = 2000) {
    const label = buttonLabel(button);
    const existing = TemporaryButtonLabels.get(button);
    if (existing) {
        clearTimeout(existing.timer);
    }

    const previousText = existing?.previousText ?? label.textContent;
    label.textContent = labelText;

    const timer = setTimeout(() => {
        label.textContent = previousText;
        TemporaryButtonLabels.delete(button);
    }, durationMs);
    TemporaryButtonLabels.set(button, {previousText, timer});
}

export function show(element) {
    element.classList.remove("hidden");
}

export function hide(element) {
    element.classList.add("hidden");
}

export function focusElement(element) {
    if (typeof element.focus === "function") {
        element.focus();
    }
}

export function setError(errorPanel, messageElement, message, options = {}) {
    messageElement.textContent = message;
    show(errorPanel);
    if (options.focus) {
        focusElement(errorPanel);
    }
}

export function clearError(errorPanel, messageElement) {
    messageElement.textContent = "";
    hide(errorPanel);
}

export function displayMessageFor(error, fallbackMessage) {
    if (error instanceof UserVisibleError) {
        return error.message;
    }
    return fallbackMessage;
}

export async function copyText(text, clipboardRef = navigator.clipboard) {
    if (!text) {
        throw new UserVisibleError("There is nothing to copy yet.");
    }
    if (!clipboardRef?.writeText) {
        throw new UserVisibleError("Clipboard access is unavailable in this browser.");
    }
    await clipboardRef.writeText(text);
}

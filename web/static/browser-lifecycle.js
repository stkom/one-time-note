import {clearAnchor, getSecretFragmentFromAnchor} from "./crypto-utils.js";

const SENSITIVE_FIELD_IDS = [
    "note-content",
    "note-link",
    "decrypted-content",
];

export function initBrowserLifecycle(root = document, options = {}) {
    const deps = {
        history: options.history ?? window.history,
        location: options.location ?? window.location,
        window: options.window ?? window,
    };

    if (!root.querySelector('[data-page="view-note"]')) {
        const fragment = getSecretFragmentFromAnchor(deps.location);
        if (fragment?.startsWith("v1.")) {
            clearAnchor(deps.history, deps.location);
        }
    }

    deps.window.addEventListener("pagehide", () => {
        for (const id of SENSITIVE_FIELD_IDS) {
            const field = root.getElementById(id);
            if (field) {
                field.value = "";
            }
        }
    });
    deps.window.addEventListener("pageshow", event => {
        if (event.persisted) {
            deps.location.reload();
        }
    });
}

initBrowserLifecycle();

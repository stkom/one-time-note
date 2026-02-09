const KEY_BYTE_LENGTH = 32;
const BURN_TOKEN_BYTE_LENGTH = 32;
const IV_BYTE_LENGTH = 12;
const GCM_TAG_BYTE_LENGTH = 16;
const ENCRYPTED_PAYLOAD_PROTECTED_HEADER_JSON = '{"v":1,"alg":"A256GCM"}';
const BASE64URL_PATTERN = /^[A-Za-z0-9_-]*$/;

export class UserVisibleError extends Error {
    constructor(message) {
        super(message);
        this.name = "UserVisibleError";
    }
}

export function getSecretFragmentFromAnchor(locationRef = window.location) {
    const hash = locationRef.hash;
    if (hash && hash.length > 1) {
        return hash.slice(1);
    }
    return null;
}

export function clearAnchor(historyRef = window.history, locationRef = window.location) {
    historyRef.replaceState(null, "", locationRef.pathname + locationRef.search);
}

export function generateKeyBytes(cryptoRef = globalThis.crypto) {
    return generateRandomBytes(KEY_BYTE_LENGTH, cryptoRef);
}

export function generateBurnTokenBytes(cryptoRef = globalThis.crypto) {
    return generateRandomBytes(BURN_TOKEN_BYTE_LENGTH, cryptoRef);
}

function generateRandomBytes(length, cryptoRef) {
    ensureCrypto(cryptoRef);
    const bytes = new Uint8Array(length);
    cryptoRef.getRandomValues(bytes);
    return bytes;
}

export async function importAesGcmKey(keyBytes, cryptoRef = globalThis.crypto) {
    ensureCrypto(cryptoRef);
    assertBytes(keyBytes, "keyBytes");
    if (keyBytes.length !== KEY_BYTE_LENGTH) {
        throw new TypeError("AES-GCM keys must be 32 bytes.");
    }

    return cryptoRef.subtle.importKey(
        "raw",
        keyBytes,
        {name: "AES-GCM"},
        false,
        ["encrypt", "decrypt"]
    );
}

export function decode(base64String) {
    if (typeof base64String !== "string" || base64String.length === 0) {
        throw new TypeError("Expected a non-empty base64url string.");
    }
    if (!BASE64URL_PATTERN.test(base64String) || base64String.length % 4 === 1) {
        throw new UserVisibleError("The decryption key is not valid base64url data.");
    }

    if (typeof Uint8Array.fromBase64 === "function") {
        try {
            return Uint8Array.fromBase64(base64String, {alphabet: "base64url"});
        } catch {
            throw new UserVisibleError("The decryption key is not valid base64url data.");
        }
    }

    const padded = base64String.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(base64String.length / 4) * 4, "=");
    try {
        return Uint8Array.from(atob(padded), ch => ch.charCodeAt(0));
    } catch {
        throw new UserVisibleError("The decryption key is not valid base64url data.");
    }
}

export function decodeKey(base64String) {
    const keyBytes = decode(base64String);
    if (keyBytes.length !== KEY_BYTE_LENGTH) {
        throw new UserVisibleError("The decryption key has the wrong length.");
    }
    return keyBytes;
}

export function decodeBurnToken(base64String) {
    const tokenBytes = decode(base64String);
    if (tokenBytes.length !== BURN_TOKEN_BYTE_LENGTH) {
        throw new UserVisibleError("The burn token has the wrong length.");
    }
    return tokenBytes;
}

export function encode(data) {
    assertBytes(data, "data");

    if (typeof data.toBase64 === "function") {
        return data.toBase64({alphabet: "base64url", omitPadding: true});
    }

    const binary = Array.from(data, byte => String.fromCharCode(byte)).join("");
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

export function encodeSecretFragment(keyBytes, burnTokenBytes) {
    assertBytes(keyBytes, "keyBytes");
    assertBytes(burnTokenBytes, "burnTokenBytes");
    if (keyBytes.length !== KEY_BYTE_LENGTH) {
        throw new TypeError("AES-GCM keys must be 32 bytes.");
    }
    if (burnTokenBytes.length !== BURN_TOKEN_BYTE_LENGTH) {
        throw new TypeError("Burn tokens must be 32 bytes.");
    }

    const json = JSON.stringify({k: encode(keyBytes), b: encode(burnTokenBytes)});
    return `v1.${encode(new TextEncoder().encode(json))}`;
}

export function parseSecretFragment(fragment) {
    if (typeof fragment !== "string" || !fragment.startsWith("v1.")) {
        throw new UserVisibleError("This note link uses an unsupported secret format.");
    }
    let parsed;
    try {
        const jsonBytes = decode(fragment.slice(3));
        parsed = JSON.parse(new TextDecoder().decode(jsonBytes));
    } catch {
        throw new UserVisibleError("The note secret in this URL is invalid.");
    }

    const keys = Object.keys(parsed).sort();
    if (keys.length !== 2 || keys[0] !== "b" || keys[1] !== "k") {
        throw new UserVisibleError("The note secret in this URL is invalid.");
    }
    if (typeof parsed.k !== "string" || typeof parsed.b !== "string") {
        throw new UserVisibleError("The note secret in this URL is invalid.");
    }
    return {
        keyBytes: decodeKey(parsed.k),
        burnToken: parsed.b,
        burnTokenBytes: decodeBurnToken(parsed.b),
    };
}

export async function encryptString(text, keyBytes, aadText, cryptoRef = globalThis.crypto) {
    if (typeof text !== "string") {
        throw new TypeError("text must be a string.");
    }
    const protectedHeader = encodeProtectedHeader();
    const additionalData = encodeAuthenticatedData(aadText, protectedHeader);
    const key = await importAesGcmKey(keyBytes, cryptoRef);
    const plaintext = new TextEncoder().encode(text);
    const iv = cryptoRef.getRandomValues(new Uint8Array(IV_BYTE_LENGTH));

    const ciphertext = await cryptoRef.subtle.encrypt(
        {name: "AES-GCM", iv, additionalData},
        key,
        plaintext
    );

    return {
        protected: protectedHeader,
        iv: encode(iv),
        ct: encode(new Uint8Array(ciphertext)),
    };
}

export async function decryptString(payload, keyBytes, aadText, cryptoRef = globalThis.crypto) {
    const encryptedPayload = decodeEncryptedPayload(payload);

    const key = await importAesGcmKey(keyBytes, cryptoRef);
    const additionalData = encodeAuthenticatedData(aadText, encryptedPayload.protected);

    const plaintext = await cryptoRef.subtle.decrypt(
        {name: "AES-GCM", iv: encryptedPayload.iv, additionalData},
        key,
        encryptedPayload.ciphertext
    );

    return new TextDecoder().decode(plaintext);
}

function decodeEncryptedPayload(payload) {
    if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
        throw new UserVisibleError("The encrypted note payload is malformed.");
    }
    const keys = Object.keys(payload).sort();
    if (keys.length !== 3 || keys[0] !== "ct" || keys[1] !== "iv" || keys[2] !== "protected") {
        throw new UserVisibleError("The encrypted note payload is malformed.");
    }
    if (typeof payload.protected !== "string" || typeof payload.iv !== "string" || typeof payload.ct !== "string") {
        throw new UserVisibleError("The encrypted note payload is malformed.");
    }
    if (payload.protected !== encodeProtectedHeader()) {
        throw new UserVisibleError("This encrypted note payload uses an unsupported format.");
    }

    const iv = decode(payload.iv);
    if (iv.length !== IV_BYTE_LENGTH) {
        throw new UserVisibleError("The encrypted note payload is malformed.");
    }
    const ciphertext = decode(payload.ct);
    if (ciphertext.length <= GCM_TAG_BYTE_LENGTH) {
        throw new UserVisibleError("The encrypted note payload is malformed.");
    }
    return {protected: payload.protected, iv, ciphertext};
}

function ensureCrypto(cryptoRef) {
    if (!cryptoRef?.getRandomValues || !cryptoRef?.subtle) {
        throw new UserVisibleError("Secure browser crypto is unavailable.");
    }
}

function assertBytes(value, name) {
    if (!value || value.constructor?.name !== "Uint8Array" || typeof value.length !== "number") {
        throw new TypeError(`${name} must be a Uint8Array.`);
    }
}

function encodeProtectedHeader() {
    return encode(new TextEncoder().encode(ENCRYPTED_PAYLOAD_PROTECTED_HEADER_JSON));
}

function encodeAuthenticatedData(aadText, protectedHeader) {
    if (typeof aadText !== "string" || aadText.length === 0) {
        throw new TypeError("aadText must be a non-empty string.");
    }
    return new TextEncoder().encode(`${aadText}.${protectedHeader}`);
}

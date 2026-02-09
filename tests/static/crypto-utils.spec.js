import {describe, expect, it, vi} from 'vitest';
import {
    clearAnchor,
    decode,
    decodeBurnToken,
    decodeKey,
    decryptString,
    encode,
    encodeSecretFragment,
    encryptString,
    generateBurnTokenBytes,
    generateKeyBytes,
    parseSecretFragment,
    importAesGcmKey,
    UserVisibleError
} from '../../web/static/crypto-utils.js';

describe('crypto-utils.js', () => {
    describe('clearAnchor', () => {
        it('should clear the hash using history.replaceState', () => {
            const replaceStateSpy = vi.spyOn(window.history, 'replaceState');
            window.location.hash = '#test';

            clearAnchor();

            expect(replaceStateSpy).toHaveBeenCalledWith(null, "", window.location.pathname + window.location.search);
            replaceStateSpy.mockRestore();
        });
    });

    describe('Crypto functions', () => {
        it('generateKeyBytes should return 32 random bytes', () => {
            const keyBytes = generateKeyBytes();
            expect(keyBytes).toBeInstanceOf(Uint8Array);
            expect(keyBytes.length).toBe(32);

            const anotherKeyBytes = generateKeyBytes();
            expect(keyBytes).not.toEqual(anotherKeyBytes);
        });

        it('generateBurnTokenBytes should return 32 random bytes', () => {
            const tokenBytes = generateBurnTokenBytes();
            expect(tokenBytes).toBeInstanceOf(Uint8Array);
            expect(tokenBytes.length).toBe(32);
        });

        it('generateKeyBytes should fail if secure crypto is unavailable', () => {
            expect(() => generateKeyBytes({})).toThrow(UserVisibleError);
        });

        it('importAesGcmKey should return a CryptoKey', async () => {
            const keyBytes = new Uint8Array(32);
            const key = await importAesGcmKey(keyBytes);
            expect(key.type).toBe('secret');
            expect(key.algorithm.name).toBe('AES-GCM');
        });

        it('importAesGcmKey should reject wrong key sizes', async () => {
            await expect(importAesGcmKey(new Uint8Array(16))).rejects.toThrow("32 bytes");
        });

        it('encode and decode should round-trip base64url data', () => {
            const bytes = new Uint8Array([0, 1, 2, 250, 251, 252, 253, 254, 255]);
            const encoded = encode(bytes);

            expect(encoded).toMatch(/^[A-Za-z0-9_-]+$/);
            expect(encoded).not.toContain('=');
            expect(decode(encoded)).toEqual(bytes);
        });

        it('decode should reject invalid base64url strings with a user-visible error', () => {
            expect(() => decode('not valid!')).toThrow(UserVisibleError);
            expect(() => decode('a')).toThrow(UserVisibleError);
        });

        it('decodeKey should enforce 32-byte keys', () => {
            expect(() => decodeKey(encode(new Uint8Array(31)))).toThrow(UserVisibleError);
            expect(decodeKey(encode(new Uint8Array(32))).length).toBe(32);
        });

        it('decodeBurnToken should enforce 32-byte tokens', () => {
            expect(() => decodeBurnToken(encode(new Uint8Array(31)))).toThrow(UserVisibleError);
            expect(decodeBurnToken(encode(new Uint8Array(32))).length).toBe(32);
        });

        it('encodeSecretFragment and parseSecretFragment should round-trip key and burn token', () => {
            const keyBytes = new Uint8Array(32).fill(1);
            const burnTokenBytes = new Uint8Array(32).fill(2);
            const fragment = encodeSecretFragment(keyBytes, burnTokenBytes);

            expect(fragment).toMatch(/^v1\.[A-Za-z0-9_-]+$/);
            const parsed = parseSecretFragment(fragment);
            expect(parsed.keyBytes).toEqual(keyBytes);
            expect(parsed.burnTokenBytes).toEqual(burnTokenBytes);
            expect(parsed.burnToken).toBe(encode(burnTokenBytes));
        });

        it('parseSecretFragment should reject unknown fields', () => {
            const payload = new TextEncoder().encode(JSON.stringify({k: encode(new Uint8Array(32)), b: encode(new Uint8Array(32)), x: "no"}));
            expect(() => parseSecretFragment(`v1.${encode(payload)}`)).toThrow(UserVisibleError);
        });

        it('encryptString and decryptString should work together', async () => {
            const text = "Hello, secret world!";
            const keyBytes = new Uint8Array(32).fill(1);
            const aad = "context-data";

            const encrypted = await encryptString(text, keyBytes, aad);
            expect(encrypted).toEqual({
                protected: expect.stringMatching(/^[A-Za-z0-9_-]+$/),
                iv: expect.stringMatching(/^[A-Za-z0-9_-]+$/),
                ct: expect.stringMatching(/^[A-Za-z0-9_-]+$/),
            });
            expect(JSON.parse(new TextDecoder().decode(decode(encrypted.protected)))).toEqual({v: 1, alg: 'A256GCM'});
            expect(decode(encrypted.iv).length).toBe(12);
            expect(decode(encrypted.ct).length).toBeGreaterThan(16);

            const decrypted = await decryptString(encrypted, keyBytes, aad);
            expect(decrypted).toBe(text);
        });

        it('decryptString should fail with wrong key', async () => {
            const text = "Hello, secret world!";
            const keyBytes = new Uint8Array(32).fill(1);
            const wrongKeyBytes = new Uint8Array(32).fill(2);
            const aad = "context-data";

            const encrypted = await encryptString(text, keyBytes, aad);
            await expect(decryptString(encrypted, wrongKeyBytes, aad)).rejects.toThrow();
        });

        it('decryptString should fail with wrong AAD', async () => {
            const text = "Hello, secret world!";
            const keyBytes = new Uint8Array(32).fill(1);
            const aad = "context-data";
            const wrongAad = "wrong-context";

            const encrypted = await encryptString(text, keyBytes, aad);
            await expect(decryptString(encrypted, keyBytes, wrongAad)).rejects.toThrow();
        });

        it('decryptString should reject malformed payloads before crypto work', async () => {
            const keyBytes = new Uint8Array(32).fill(1);
            const validPayload = await encryptString("secret", keyBytes, "context-data");

            await expect(decryptString(new Uint8Array(12), keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
            await expect(decryptString({...validPayload, protected: encode(new TextEncoder().encode('{"v":2,"alg":"A256GCM"}'))}, keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
            await expect(decryptString({...validPayload, protected: encode(new TextEncoder().encode('{"v":1,"alg":"A128GCM"}'))}, keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
            await expect(decryptString({...validPayload, protected: "not valid!"}, keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
            await expect(decryptString({...validPayload, iv: encode(new Uint8Array(11))}, keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
            await expect(decryptString({...validPayload, ct: encode(new Uint8Array(16))}, keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
            await expect(decryptString({...validPayload, extra: "no"}, keyBytes, "context-data")).rejects.toThrow(UserVisibleError);
        });
    });
});

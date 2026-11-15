import CryptoKit
import Foundation

/// The kid device's identity key: a P-256 signing key in the Secure Enclave. The
/// private key never leaves the chip; the Keychain holds only the Secure Enclave's
/// opaque, device-bound wrapping of it (`dataRepresentation`), which is useless on
/// any other device. No bearer secret is stored or sent.
///
/// The key identifies the device, never the child: the server maps the device to
/// a child and can reassign it.
protocol DeviceSigningKey: Sendable {
    /// Uncompressed X9.63 point (65 bytes, 0x04 || X || Y).
    var publicKeyX963: Data { get }
    /// SubjectPublicKeyInfo DER.
    var publicKeySPKI: Data { get }
    /// ECDSA P-256 over SHA-256 of `message`; DER-encoded signature.
    func signatureDER(for message: Data) throws -> Data
    /// Same signature as raw r || s (64 bytes).
    func signatureRaw(for message: Data) throws -> Data
}

struct SecureEnclaveDeviceKey: DeviceSigningKey {
    let key: SecureEnclave.P256.Signing.PrivateKey

    var publicKeyX963: Data { key.publicKey.x963Representation }
    var publicKeySPKI: Data { key.publicKey.derRepresentation }

    func signatureDER(for message: Data) throws -> Data { try key.signature(for: message).derRepresentation }
    func signatureRaw(for message: Data) throws -> Data { try key.signature(for: message).rawRepresentation }
}

/// Software key with the same interface, for tests and the simulator only.
struct SoftwareDeviceKey: DeviceSigningKey {
    let key: P256.Signing.PrivateKey

    var publicKeyX963: Data { key.publicKey.x963Representation }
    var publicKeySPKI: Data { key.publicKey.derRepresentation }

    func signatureDER(for message: Data) throws -> Data { try key.signature(for: message).derRepresentation }
    func signatureRaw(for message: Data) throws -> Data { try key.signature(for: message).rawRepresentation }
}

/// Loads, creates and forgets the device key.
struct DeviceKeyStore: Sendable {
    private static let account = "device-key.v1"
    var keychain = Keychain()

    enum KeyError: LocalizedError {
        case secureEnclaveUnavailable

        var errorDescription: String? {
            "This device has no Secure Enclave, so it can't be paired as a child's device."
        }
    }

    func load() -> (any DeviceSigningKey)? {
        guard let blob = keychain.data(for: Self.account) else { return nil }
        if SecureEnclave.isAvailable, let key = try? SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: blob) {
            return SecureEnclaveDeviceKey(key: key)
        }
        #if targetEnvironment(simulator)
        if let key = try? P256.Signing.PrivateKey(rawRepresentation: blob) { return SoftwareDeviceKey(key: key) }
        #endif
        return nil
    }

    /// A fresh key for a new pairing, not yet saved: the current pairing stays
    /// intact until the server accepts the new key. Usable after the first unlock
    /// since boot (so a sync as the screen locks still works), never synced or
    /// backed up.
    func generate() throws -> (key: any DeviceSigningKey, blob: Data) {
        if SecureEnclave.isAvailable {
            var error: Unmanaged<CFError>?
            guard let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly, .privateKeyUsage, &error) else {
                throw error!.takeRetainedValue() as Error
            }
            let key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
            return (SecureEnclaveDeviceKey(key: key), key.dataRepresentation)
        }
        #if targetEnvironment(simulator)
        let key = P256.Signing.PrivateKey()
        return (SoftwareDeviceKey(key: key), key.rawRepresentation)
        #else
        throw KeyError.secureEnclaveUnavailable
        #endif
    }

    func save(_ blob: Data) {
        keychain.set(blob, for: Self.account)
    }

    func delete() {
        keychain.set(nil as Data?, for: Self.account)
    }
}

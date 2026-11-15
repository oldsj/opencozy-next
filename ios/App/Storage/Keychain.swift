import CryptoKit
import Foundation
import Security

/// Minimal generic-password Keychain wrapper for the app's own secrets.
/// Items are device-only and readable after first unlock, so they never sync to
/// iCloud or move to another device in a backup.
struct Keychain: Sendable {
    var service = "org.opencozy.ios"

    func data(for account: String) -> Data? {
        var query = base(account)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess else { return nil }
        return result as? Data
    }

    func string(for account: String) -> String? {
        data(for: account).flatMap { String(data: $0, encoding: .utf8) }
    }

    @discardableResult
    func set(_ data: Data?, for account: String) -> Bool {
        SecItemDelete(base(account) as CFDictionary)
        guard let data else { return true }
        var item = base(account)
        item[kSecValueData as String] = data
        item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        return SecItemAdd(item as CFDictionary, nil) == errSecSuccess
    }

    @discardableResult
    func set(_ string: String?, for account: String) -> Bool {
        set(string.map { Data($0.utf8) }, for: account)
    }

    private func base(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: account]
    }
}

/// A parent PIN that gates the setup screen. Stored as a salted SHA-256 hash in the
/// Keychain. It keeps a child out of the settings; it isn't meant to resist someone
/// with the device's passcode and debugging tools.
struct ParentPIN: Sendable {
    static let account = "parent-pin.v1"
    static let throttleAccount = "parent-pin-throttle.v1"
    /// Wrong attempts allowed before lockouts start.
    static let freeAttempts = 5
    static let maxLockout: TimeInterval = 60 * 60
    var keychain = Keychain()

    enum Attempt: Equatable {
        case success
        case wrong(lockedUntil: Date?)
        case throttled(until: Date)
    }

    /// Failed-attempt state, kept in the Keychain so closing and reopening the
    /// parent sheet (or relaunching the app) doesn't reset it.
    struct Throttle: Codable, Equatable {
        var failures = 0
        var lockedUntil: Date?
    }

    var throttle: Throttle {
        keychain.data(for: Self.throttleAccount).flatMap { try? JSONDecoder().decode(Throttle.self, from: $0) } ?? Throttle()
    }

    /// After `freeAttempts` failures each further failure locks entry for 30 s,
    /// doubling up to an hour.
    static func lockout(afterFailures failures: Int) -> TimeInterval? {
        guard failures >= freeAttempts else { return nil }
        let exponent = min(failures - freeAttempts, 7)
        return min(maxLockout, 30 * pow(2, Double(exponent)))
    }

    /// Checks a PIN, honouring and updating the persisted throttle.
    func attempt(_ pin: String, now: Date = Date()) -> Attempt {
        var state = throttle
        if let until = state.lockedUntil, until > now { return .throttled(until: until) }
        if verify(pin) {
            keychain.set(nil as Data?, for: Self.throttleAccount)
            return .success
        }
        state.failures += 1
        state.lockedUntil = Self.lockout(afterFailures: state.failures).map { now.addingTimeInterval($0) }
        keychain.set(try? JSONEncoder().encode(state), for: Self.throttleAccount)
        return .wrong(lockedUntil: state.lockedUntil)
    }

    var isSet: Bool { keychain.data(for: Self.account) != nil }

    static func isValid(_ pin: String) -> Bool {
        (4...8).contains(pin.count) && pin.allSatisfy(\.isASCIIDigit)
    }

    @discardableResult
    func set(_ pin: String) -> Bool {
        guard Self.isValid(pin) else { return false }
        var salt = Data(count: 16)
        let status = salt.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, 16, $0.baseAddress!) }
        guard status == errSecSuccess else { return false }
        keychain.set(nil as Data?, for: Self.throttleAccount)
        return keychain.set(salt + Self.hash(pin, salt: salt), for: Self.account)
    }

    func verify(_ pin: String) -> Bool {
        guard let stored = keychain.data(for: Self.account), stored.count == 48 else { return false }
        let salt = stored.prefix(16)
        let expected = stored.suffix(32)
        let actual = Self.hash(pin, salt: Data(salt))
        // Constant-time comparison.
        return zip(actual, expected).reduce(0) { $0 | ($1.0 ^ $1.1) } == 0
    }

    static func hash(_ pin: String, salt: Data) -> Data {
        Data(SHA256.hash(data: salt + Data(pin.utf8)))
    }
}

private extension Character {
    var isASCIIDigit: Bool { ("0"..."9").contains(self) }
}

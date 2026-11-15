import Foundation

/// Kid-device settings. The server origin and device ID aren't secret and live in
/// UserDefaults; the device key lives in the Secure Enclave (wrapped in the
/// Keychain). None of it is shared with the extensions.
struct AppConfig: Sendable {
    private static let baseURLKey = "baseURL.v2"
    private static let deviceIdKey = "deviceId.v1"

    var keys = DeviceKeyStore()
    var clock = DeviceClock()
    /// Tests use their own suite.
    var defaultsSuite: String?
    private var defaults: UserDefaults { defaultsSuite.flatMap(UserDefaults.init(suiteName:)) ?? .standard }

    var baseURL: URL? {
        get { defaults.string(forKey: Self.baseURLKey).flatMap(URL.init(string:)) }
        nonmutating set { defaults.set(newValue?.absoluteString, forKey: Self.baseURLKey) }
    }

    var deviceId: String? {
        get { defaults.string(forKey: Self.deviceIdKey) }
        nonmutating set { defaults.set(newValue, forKey: Self.deviceIdKey) }
    }

    /// The signer for this device, if it's paired and its key is readable.
    var signer: DeviceRequestSigner? {
        guard let deviceId, let key = keys.load() else { return nil }
        return DeviceRequestSigner(deviceId: deviceId, key: key, clock: clock)
    }

    var isConfigured: Bool { baseURL != nil && signer != nil }

    /// Phase 1 kept a bearer API key (`ocz_…`) in the Keychain and the server URL
    /// under `baseURL.v1`. Pairing replaced both; delete them so no bearer secret
    /// stays on the device. Safe to call repeatedly.
    static let legacyTokenAccount = "api-credential.v1"
    static let legacyBaseURLKey = "baseURL.v1"

    func purgeLegacyCredential() {
        keys.keychain.set(nil as String?, for: Self.legacyTokenAccount)
        defaults.removeObject(forKey: Self.legacyBaseURLKey)
    }

    /// Forgets the pairing on this device. The server-side device stays until an
    /// adult revokes it in Manage → Devices.
    func forgetPairing() {
        deviceId = nil
        baseURL = nil
        keys.delete()
    }
}

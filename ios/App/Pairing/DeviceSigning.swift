import CryptoKit
import Foundation

/// Signs `/api/v1` requests with the device key (contract "Signed requests").
/// No bearer secret exists: every request carries the device ID, a timestamp, a
/// one-time nonce and an ECDSA P-256 signature over a canonical string.
struct DeviceRequestSigner: Sendable {
    static let version = "OPENCOZY-DEVICE-V1"

    let deviceId: String
    let key: any DeviceSigningKey
    var clock: DeviceClock = DeviceClock()

    /// The exact string the server rebuilds: 7 lines, no trailing newline.
    static func canonical(method: String, pathAndQuery: String, timestamp: Int, nonce: String, body: Data, deviceId: String) -> String {
        let bodyHash = SHA256.hash(data: body).map { String(format: "%02x", $0) }.joined()
        return [version, method.uppercased(), pathAndQuery, String(timestamp), nonce, bodyHash, deviceId.lowercased()]
            .joined(separator: "\n")
    }

    /// Re-encodes the query so every byte outside RFC 3986 unreserved characters
    /// (plus the `=`/`&` separators and existing `%XX` escapes) is percent-encoded.
    /// Foundation leaves characters like `'` raw where WHATWG `url.search` (what the
    /// server signs) would encode them; with everything pre-encoded both agree.
    static func strictQuery(_ url: URL) -> URL {
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let query = components.percentEncodedQuery, !query.isEmpty else { return url }
        var allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~=&")
        allowed.insert("%")
        let encoded = query.addingPercentEncoding(withAllowedCharacters: allowed) ?? query
        guard encoded != query else { return url }
        components.percentEncodedQuery = encoded
        return components.url ?? url
    }

    /// Path plus `?query`, percent-encoded exactly as it goes on the wire (the
    /// server signs `url.pathname + url.search`).
    static func pathAndQuery(of url: URL) -> String {
        let path = url.path(percentEncoded: true)
        guard let query = url.query(percentEncoded: true), !query.isEmpty else { return path.isEmpty ? "/" : path }
        return (path.isEmpty ? "/" : path) + "?" + query
    }

    /// 32 base64url characters, inside the server's 16–64 `[A-Za-z0-9_-]` rule.
    static func newNonce() -> String {
        var bytes = [UInt8](repeating: 0, count: 24)
        for i in bytes.indices { bytes[i] = UInt8.random(in: 0...255) }
        return Data(bytes).base64URL
    }

    /// Adds the four signature headers. Never sets `Authorization`.
    func sign(_ request: inout URLRequest, nonce: String = newNonce()) throws {
        guard let original = request.url else { return }
        let url = Self.strictQuery(original)
        request.url = url
        let timestamp = Int(clock.now().timeIntervalSince1970.rounded(.down))
        let message = Self.canonical(method: request.httpMethod ?? "GET", pathAndQuery: Self.pathAndQuery(of: url),
                                     timestamp: timestamp, nonce: nonce, body: request.httpBody ?? Data(), deviceId: deviceId)
        let signature = try key.signatureRaw(for: Data(message.utf8))
        request.setValue(nil, forHTTPHeaderField: "Authorization")
        request.setValue(deviceId.lowercased(), forHTTPHeaderField: "X-OpenCozy-Device")
        request.setValue(String(timestamp), forHTTPHeaderField: "X-OpenCozy-Timestamp")
        request.setValue(nonce, forHTTPHeaderField: "X-OpenCozy-Nonce")
        request.setValue(signature.base64URL, forHTTPHeaderField: "X-OpenCozy-Signature")
    }
}

/// Server time = local time + offset. The offset comes from `serverTime` in the
/// pairing and device responses, or from a `Date` header after a 401, so a device
/// whose clock is off still signs inside the server's 60-second window.
struct DeviceClock: Sendable {
    private static let key = "device.clockOffset.v1"
    var defaultsSuite: String?

    private var defaults: UserDefaults { defaultsSuite.flatMap(UserDefaults.init(suiteName:)) ?? .standard }

    var offset: TimeInterval {
        get { defaults.double(forKey: Self.key) }
        nonmutating set { defaults.set(newValue, forKey: Self.key) }
    }

    func now(local: Date = Date()) -> Date { local.addingTimeInterval(offset) }

    /// Records the server's time as observed at `receivedAt` (local).
    func observe(serverTime: Date, receivedAt: Date) {
        offset = serverTime.timeIntervalSince(receivedAt)
    }

    /// Parses an HTTP `Date` header (RFC 7231 IMF-fixdate).
    static func parseHTTPDate(_ text: String) -> Date? {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "GMT")
        formatter.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
        return formatter.date(from: text)
    }
}

/// What a pairing QR code or typed code gives the app.
enum PairingInput: Equatable, Sendable {
    case token(origin: URL, token: String)
    case code(origin: URL, code: String)

    var origin: URL {
        switch self {
        case .token(let origin, _), .code(let origin, _): origin
        }
    }

    static let codeAlphabet = Set("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

    /// `opencozy-pair:v1?origin=<percent-encoded origin>&token=<43 base64url>`.
    static func parseQR(_ payload: String) -> PairingInput? {
        let trimmed = payload.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let components = URLComponents(string: trimmed),
              components.scheme == "opencozy-pair", components.path == "v1",
              let items = components.queryItems,
              let originText = items.first(where: { $0.name == "origin" })?.value,
              let origin = BaseURL.parse(originText), originIsBare(originText),
              let token = items.first(where: { $0.name == "token" })?.value,
              token.count == 43, token.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" }), token.allSatisfy(\.isASCII)
        else { return nil }
        return .token(origin: origin, token: token)
    }

    /// The fallback code, with or without the dash, any case. Sent as `XXXX-XXXX`.
    static func parseCode(_ text: String, origin originText: String) -> PairingInput? {
        let raw = text.uppercased().filter { $0 != "-" && !$0.isWhitespace }
        guard raw.count == 8, raw.allSatisfy(codeAlphabet.contains), let origin = BaseURL.parse(originText) else { return nil }
        return .code(origin: origin, code: "\(raw.prefix(4))-\(raw.suffix(4))")
    }

    /// The QR origin must be just scheme://host[:port]; anything else isn't ours.
    private static func originIsBare(_ text: String) -> Bool {
        guard let c = URLComponents(string: text) else { return false }
        return (c.path.isEmpty || c.path == "/") && c.query == nil && c.fragment == nil && c.user == nil
    }
}

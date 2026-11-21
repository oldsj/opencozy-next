import Foundation
import WebKit

/// Who the app's persistent web data (cookies, local storage, caches) belongs to.
///
/// Every web view shares `WKWebsiteDataStore.default()`. The server resolves an
/// adult cookie before anything else, so an adult sign-in left over from parent
/// mode would make the kid web view act as the adult. So the store is wiped:
/// - before a web view loads for a different owner than last time (role, server
///   or pairing changed, even across a crash or relaunch), and
/// - straight away when the role changes, on unpair and lock, and when a parent
///   phone forgets its server.
///
/// Operations run in call order, so a wipe requested by a role switch always
/// finishes before the next web view's `prepare` lets it load.
@MainActor
final class WebDataGate {
    enum Owner: Equatable {
        case kid(deviceId: String, origin: URL)
        case parent(origin: URL)

        var key: String {
            switch self {
            case .kid(let deviceId, let origin): "kid|\(deviceId)|\(origin.absoluteString)"
            case .parent(let origin): "parent|\(origin.absoluteString)"
            }
        }
    }

    static let shared = WebDataGate()
    private static let ownerKey = "webData.owner.v1"

    private let defaults: UserDefaults
    private let clearStore: @MainActor () async -> Void
    private var queue: Task<Void, Never>?

    init(defaults: UserDefaults = .standard, clear: @escaping @MainActor () async -> Void = WebDataGate.clearDefaultStore) {
        self.defaults = defaults
        self.clearStore = clear
    }

    var owner: String? { defaults.string(forKey: Self.ownerKey) }

    /// Call before a web view's first load. Wipes the store unless it already
    /// belongs to `owner`.
    func prepare(for owner: Owner) async {
        await enqueue { [self] in
            guard self.owner != owner.key else { return }
            await clearStore()
            defaults.set(owner.key, forKey: Self.ownerKey)
        }.value
    }

    /// Role change, unpair, lock, forgetting the server: wipe now. Ordered before
    /// any later `prepare`.
    @discardableResult
    func reset() -> Task<Void, Never> {
        defaults.removeObject(forKey: Self.ownerKey)
        return enqueue { [self] in await clearStore() }
    }

    private func enqueue(_ operation: @escaping @MainActor () async -> Void) -> Task<Void, Never> {
        let previous = queue
        let task = Task { @MainActor in
            await previous?.value
            await operation()
        }
        queue = task
        return task
    }

    static func clearDefaultStore() async {
        await WKWebsiteDataStore.default().removeData(ofTypes: WKWebsiteDataStore.allWebsiteDataTypes(), modifiedSince: .distantPast)
    }
}

/// Cookies from the web view's store, for the one native call that uses the web
/// sign-in (`POST /api/phone/sessions/web`).
enum WebCookies {
    /// The kid device's web-session cookie (`internal/actor` DeviceWebCookie).
    static let deviceCookieNames: Set<String> = ["__Host-device", "opencozy-device", "__Host-opencozy-device"]

    /// The cookies a browser would send to `url`.
    static func matching(_ cookies: [HTTPCookie], url: URL, now: Date = Date()) -> [HTTPCookie] {
        guard let host = url.host()?.lowercased() else { return [] }
        let path = url.path(percentEncoded: true).isEmpty ? "/" : url.path(percentEncoded: true)
        let https = url.scheme?.lowercased() == "https"
        return cookies.filter { cookie in
            let domain = cookie.domain.lowercased()
            let domainMatches = domain.hasPrefix(".")
                ? host == String(domain.dropFirst()) || host.hasSuffix(domain)
                : host == domain
            let cookiePath = cookie.path.isEmpty ? "/" : cookie.path
            let pathMatches = path == cookiePath || path.hasPrefix(cookiePath.hasSuffix("/") ? cookiePath : cookiePath + "/")
            return domainMatches && pathMatches && (!cookie.isSecure || https) && (cookie.expiresDate.map { $0 > now } ?? true)
        }
    }

    /// The `Cookie` header for minting a parent phone session, or nil when there's
    /// nothing to send or a device web-session cookie is present (never mint a
    /// parent credential from a jar a kid session has touched).
    static func parentHeader(_ cookies: [HTTPCookie], url: URL, now: Date = Date()) -> String? {
        let sent = matching(cookies, url: url, now: now)
        guard !sent.isEmpty, !sent.contains(where: { deviceCookieNames.contains($0.name) }) else { return nil }
        return HTTPCookie.requestHeaderFields(with: sent)["Cookie"]
    }
}

/// `POST /api/phone/sessions/web`: this phone's `ocp_` notification session from the
/// adult web sign-in. Sent natively (not from page JavaScript) with the web
/// view's cookie and the canonical `Origin`, so the token never enters the page.
enum ParentSessionMint {
    static func request(origin: URL, cookie: String, deviceName: String) throws -> URLRequest {
        var request = URLRequest(url: origin.appending(path: "api/phone/sessions/web"), cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 20)
        request.httpMethod = "POST"
        request.httpShouldHandleCookies = false
        request.setValue(cookie, forHTTPHeaderField: "Cookie")
        request.setValue(originHeader(origin), forHTTPHeaderField: "Origin")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.httpBody = try JSONEncoder().encode(["deviceName": deviceName])
        return request
    }

    /// `scheme://host[:port]`, as a browser sends it (default ports left out).
    static func originHeader(_ origin: URL) -> String {
        let scheme = origin.scheme?.lowercased() ?? "https"
        let host = origin.host()?.lowercased() ?? ""
        let defaultPort = scheme == "https" ? 443 : 80
        if let port = origin.port, port != defaultPort { return "\(scheme)://\(host):\(port)" }
        return "\(scheme)://\(host)"
    }

    static func send(_ request: URLRequest, session: URLSession = .shared) async throws -> (status: Int, body: Data) {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw APIError.transport("No HTTP response") }
        return (http.statusCode, data)
    }
}

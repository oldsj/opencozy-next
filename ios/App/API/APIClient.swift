import Foundation

enum APIError: Error, Equatable, LocalizedError {
    /// The server answered with a status and an `{ "error": { "code", "message" } }`
    /// body (`message` is safe to show).
    case server(status: Int, message: String)
    /// No usable response (offline, timeout, TLS, …). Safe to retry.
    case transport(String)
    case decoding(String)
    case notConfigured

    var errorDescription: String? {
        switch self {
        case .server(let status, let message): "\(message) (\(status))"
        case .transport(let message): "Can't reach OpenCozy: \(message)"
        case .decoding(let message): "Unexpected response from OpenCozy: \(message)"
        case .notConfigured: "A parent needs to finish setup."
        }
    }

    /// Whether a command with the same request ID should be retried later.
    /// 5xx (including 503) and transport failures may not have been recorded.
    var isRetryable: Bool {
        switch self {
        case .transport: true
        case .server(let status, _): status >= 500
        case .decoding, .notConfigured: false
        }
    }

    /// The server has no such route (404/405): a call this server doesn't serve yet.
    var isMissingEndpoint: Bool {
        if case .server(let status, _) = self { return status == 404 || status == 405 }
        return false
    }
}

/// The calls the kid device makes natively. Everything else (Today, rewards,
/// redeeming) happens in the web view. A protocol so tests can use fixtures.
protocol OpenCozyAPI: Sendable {
    /// This device, its current child and the server's time.
    func device() async throws -> DeviceInfo
    /// Returns the response and the local time it arrived, for clock-skew correction.
    func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date)
    /// A one-time URL that signs the web view in as this device's child.
    func webSession() async throws -> DeviceWebSession
    /// Sends shield events recorded offline; returns the IDs the server holds.
    func screenTimeEvents(_ events: [ScreenTimeEventUpload]) async throws -> Set<String>
    /// Asks a parent for more time (a request queued on the lock screen).
    func askForTime(requestId: String) async throws -> TimeRequestStatus
    /// Registers this install's APNs token for the device's child.
    func registerPushToken(_ request: PushTokenRequest) async throws
}

extension OpenCozyAPI {
    func webSession() async throws -> DeviceWebSession { throw APIError.notConfigured }
    func screenTimeEvents(_ events: [ScreenTimeEventUpload]) async throws -> Set<String> { Set(events.map(\.id)) }
    func askForTime(requestId: String) async throws -> TimeRequestStatus { TimeRequestStatus(id: requestId, state: .pending) }
    func registerPushToken(_ request: PushTokenRequest) async throws {}
}

/// The kid device client: every request is signed by the device key.
///
/// Paths are relative to `/api`. Calls still on the previous server's `v1/` paths
/// belong to later slices (8: iPad time, 11: pushes); this server answers them 404
/// until then, and the sync treats that as "not here yet" (see `AppModel`).
struct HTTPOpenCozyAPI: OpenCozyAPI {
    let baseURL: URL
    let signer: DeviceRequestSigner
    var session: URLSession = .shared

    private var http: APITransport { APITransport(baseURL: baseURL, auth: .device(signer), session: session) }

    func device() async throws -> DeviceInfo {
        let (data, receivedAt) = try await http.send("GET", "device")
        let info = try APITransport.decode(DeviceInfo.self, from: data)
        signer.clock.observe(serverTime: info.serverTime, receivedAt: receivedAt)
        return info
    }

    func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
        let (data, receivedAt) = try await http.send("GET", "v1/screen-time", query: [URLQueryItem(name: "target", value: target.rawValue)])
        return (try APITransport.decode(ScreenTimeResponse.self, from: data), receivedAt)
    }

    func webSession() async throws -> DeviceWebSession {
        try await http.post("device/web-session", body: EmptyRequest(), as: DeviceWebSession.self)
    }

    func screenTimeEvents(_ events: [ScreenTimeEventUpload]) async throws -> Set<String> {
        Set(try await http.post("v1/device/screen-time-events", body: ScreenTimeEventsRequest(events: events), as: ScreenTimeEventsResponse.self).recorded)
    }

    func askForTime(requestId: String) async throws -> TimeRequestStatus {
        try await http.post("v1/device/time-requests", body: TimeRequestAsk(requestId: requestId), as: TimeRequestReply.self).request
    }

    func registerPushToken(_ request: PushTokenRequest) async throws {
        _ = try await http.put("device/push", body: request, headers: ["Idempotency-Key": UUID().uuidString.lowercased()], as: PushTokenResponse.self)
    }
}

/// `POST /api/v1/device/time-requests` `{ requestId }` → `{ request }` (201 new, 200 already pending).
struct TimeRequestAsk: Encodable, Sendable { var requestId: String }
struct TimeRequestReply: Decodable, Sendable { var request: TimeRequestStatus }

/// `POST /api/v1/device/screen-time-events` → `{ recorded: [id] }`.
struct ScreenTimeEventUpload: Encodable, Equatable, Sendable {
    var id: String
    var kind: String
    var secondsAgo: Int

    init(_ event: ScreenTimeEvent, now: ClockReading) {
        id = event.id
        kind = event.kind.rawValue
        secondsAgo = min(event.secondsAgo(now: now), 7 * 24 * 60 * 60)
    }
}

struct ScreenTimeEventsRequest: Encodable, Sendable { var events: [ScreenTimeEventUpload] }
struct ScreenTimeEventsResponse: Decodable, Sendable { var recorded: [String] }

/// `{}`: for POSTs whose meaning is all in the signed headers.
struct EmptyRequest: Encodable, Sendable {}

/// `POST /api/device/pair` (no auth): redeems a pairing token or code for a
/// device bound to this public key.
struct DevicePairRequest: Encodable, Equatable, Sendable {
    var token: String?
    var code: String?
    /// Standard padded base64 of the 65-byte X9.63 public key.
    var publicKey: String
    /// Proof of possession: base64url (no padding) raw r||s signature by the new key
    /// over `proofMessage`. Required by the server.
    var proof: String = ""
    /// `ios` (see `DevicePlatform`).
    var platform: String
    /// 1 to 80 characters.
    var name: String

    /// `OPENCOZY-PAIR-V1`, the token or code exactly as sent, and the public key
    /// exactly as sent: 3 lines, no trailing newline.
    static func proofMessage(secret: String, publicKey: String) -> String {
        ["OPENCOZY-PAIR-V1", secret, publicKey].joined(separator: "\n")
    }

    /// Signs the proof with the new key. Call after `token` or `code` is set.
    mutating func prove(with key: any DeviceSigningKey) throws {
        let message = Self.proofMessage(secret: token ?? code ?? "", publicKey: publicKey)
        proof = try key.signatureRaw(for: Data(message.utf8)).base64URL
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(token, forKey: .token)
        try c.encodeIfPresent(code, forKey: .code)
        try c.encode(publicKey, forKey: .publicKey)
        try c.encode(proof, forKey: .proof)
        try c.encode(platform, forKey: .platform)
        try c.encode(name, forKey: .name)
    }

    private enum CodingKeys: String, CodingKey { case token, code, publicKey, proof, platform, name }
}

protocol DevicePairingAPI: Sendable {
    func pair(origin: URL, _ request: DevicePairRequest) async throws -> (DeviceInfo, receivedAt: Date)
}

struct HTTPDevicePairingAPI: DevicePairingAPI {
    var session: URLSession = .shared

    func pair(origin: URL, _ request: DevicePairRequest) async throws -> (DeviceInfo, receivedAt: Date) {
        let (data, receivedAt) = try await APITransport(baseURL: origin, auth: .none, session: session).send("POST", "device/pair", body: request)
        return (try APITransport.decode(DeviceInfo.self, from: data), receivedAt)
    }
}

/// Small JSON-over-HTTP helper for `/api`. Errors come back as `APIError`.
/// `path` is relative to `/api`; the parent role's calls still use `v1/…` (slices 10, 11).
struct APITransport: Sendable {
    enum Auth: Sendable {
        case none
        /// Parent session token (`ocp_…`).
        case bearer(String)
        /// A paired child's device: every request signed with its Secure Enclave key.
        case device(DeviceRequestSigner)
    }

    let baseURL: URL
    var auth: Auth
    var session: URLSession = .shared

    init(baseURL: URL, auth: Auth, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.auth = auth
        self.session = session
    }

    init(baseURL: URL, token: String?, session: URLSession = .shared) {
        self.init(baseURL: baseURL, auth: token.map(Auth.bearer) ?? .none, session: session)
    }

    func get<T: Decodable>(_ path: String, as type: T.Type) async throws -> T {
        try Self.decode(type, from: try await send("GET", path).0)
    }

    func post<B: Encodable, T: Decodable>(_ path: String, body: B, headers: [String: String] = [:], as type: T.Type) async throws -> T {
        try Self.decode(type, from: try await send("POST", path, body: body, headers: headers).0)
    }

    func put<B: Encodable, T: Decodable>(_ path: String, body: B, headers: [String: String] = [:], as type: T.Type) async throws -> T {
        try Self.decode(type, from: try await send("PUT", path, body: body, headers: headers).0)
    }

    func delete(_ path: String) async throws {
        _ = try await send("DELETE", path)
    }

    /// Returns the body and the local time the response arrived.
    func send(_ method: String, _ path: String, query: [URLQueryItem] = [], body: (any Encodable)? = nil,
              headers: [String: String] = [:]) async throws -> (Data, Date) {
        var url = baseURL.appending(path: "api/\(path)")
        if !query.isEmpty { url.append(queryItems: query) }
        var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 20)
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        req.setValue(Self.clientHeader, forHTTPHeaderField: "X-OpenCozy-Client")
        for (name, value) in headers { req.setValue(value, forHTTPHeaderField: name) }
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONEncoder().encode(body)
        }
        switch auth {
        case .none:
            return try await perform(req).checked()
        case .bearer(let token):
            req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
            return try await perform(req).checked()
        case .device(let signer):
            let first = try await perform(signed(req, signer))
            // A 401 may only mean our clock is off. If the server's Date header
            // disagrees by more than 30 s, correct the offset and retry once with a
            // fresh nonce and timestamp.
            if first.status == 401, let serverNow = first.serverDate,
               abs(serverNow.timeIntervalSince(signer.clock.now(local: first.receivedAt))) > 30 {
                signer.clock.observe(serverTime: serverNow, receivedAt: first.receivedAt)
                return try await perform(signed(req, signer)).checked()
            }
            return try first.checked()
        }
    }

    private func signed(_ request: URLRequest, _ signer: DeviceRequestSigner) throws -> URLRequest {
        var copy = request
        try signer.sign(&copy)
        return copy
    }

    private struct Reply {
        var data: Data
        var status: Int
        var receivedAt: Date
        var serverDate: Date?

        func checked() throws -> (Data, Date) {
            guard (200..<300).contains(status) else {
                let message = ErrorBody.message(in: data) ?? HTTPURLResponse.localizedString(forStatusCode: status)
                throw APIError.server(status: status, message: message)
            }
            return (data, receivedAt)
        }
    }

    private func perform(_ req: URLRequest) async throws -> Reply {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: req)
        } catch {
            throw APIError.transport(error.localizedDescription)
        }
        let receivedAt = Date()
        guard let http = response as? HTTPURLResponse else { throw APIError.transport("No HTTP response") }
        let serverDate = http.value(forHTTPHeaderField: "Date").flatMap(DeviceClock.parseHTTPDate)
        return Reply(data: data, status: http.statusCode, receivedAt: receivedAt, serverDate: serverDate)
    }

    static func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        do {
            return try APIDates.decoder().decode(type, from: data)
        } catch {
            throw APIError.decoding(String(describing: error))
        }
    }

    /// `X-OpenCozy-Client: OpenCozy-iOS/<version>`; a server can answer 426 to an app too old for it.
    static let clientHeader = "OpenCozy-iOS/\(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0")"

    /// `{ "error": { "code", "message" } }`; the previous server's `{ "error": "…" }` too,
    /// while the parent role still talks to it.
    enum ErrorBody {
        private struct Current: Decodable { struct Detail: Decodable { var code: String; var message: String }; var error: Detail }
        private struct Previous: Decodable { var error: String }

        static func message(in data: Data) -> String? {
            if let body = try? JSONDecoder().decode(Current.self, from: data) { return body.error.message }
            return (try? JSONDecoder().decode(Previous.self, from: data))?.error
        }
    }
}

enum BaseURL {
    /// Normalises a server origin: requires https and a host, drops any path, query
    /// or trailing slash. Plain http is accepted only for this machine (a local dev
    /// server on the simulator), so a pairing token or parent password never crosses
    /// the network in cleartext.
    static func parse(_ text: String) -> URL? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard var components = URLComponents(string: trimmed),
              let scheme = components.scheme?.lowercased(),
              let host = components.host?.lowercased(), !host.isEmpty,
              scheme == "https" || (scheme == "http" && loopbackHosts.contains(host)) else { return nil }
        components.scheme = scheme
        components.path = ""
        components.query = nil
        components.fragment = nil
        components.user = nil
        components.password = nil
        return components.url
    }

    static let loopbackHosts: Set<String> = ["localhost", "127.0.0.1", "::1", "[::1]"]
}

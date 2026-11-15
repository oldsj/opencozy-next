import Foundation

// Parent device API. The inbox is this server's (`/api/inbox`, which a phone
// session's `ocp_` token may call). Sign-in, the phone session and push tokens are
// still the previous server's `/api/v1/parent/*` paths until slices 10 (passkeys)
// and 11 (phone sessions, pushes).

/// An inbox id as a path segment: ids are UUIDs, anything else is refused rather
/// than put in a URL.
enum InboxPath {
    static func id(_ id: String) -> String {
        UUID(uuidString: id) != nil ? id.lowercased() : "invalid"
    }
}

struct ParentSessionInfo: Decodable, Equatable, Sendable {
    var id: String
    var deviceName: String
    var createdAt: Date
    var expiresAt: Date
    var lastUsedAt: Date?
}

struct HouseholdInfo: Decodable, Equatable, Sendable {
    var id: String
    var name: String
    /// What a locked iPad says (`GET /api/v1/device` only).
    var lockMessage: String?
}

struct SignInRequest: Encodable, Sendable {
    var email: String
    var password: String
    var deviceName: String
}

struct SignInResponse: Decodable, Sendable {
    var token: String
    var session: ParentSessionInfo
    var household: HouseholdInfo
}

struct SessionResponse: Decodable, Sendable {
    var session: ParentSessionInfo
    var household: HouseholdInfo
}

/// One thing waiting for an adult (api/src/inbox.yaml `InboxItem`). The native app
/// only needs the ids and the count, to keep notifications and the badge in step; the
/// inbox itself is the web app's.
struct InboxItem: Decodable, Equatable, Identifiable, Sendable {
    enum Kind: String, Decodable, Sendable {
        case completion, routine, redemption, timeRequest = "time_request", unknown
        init(from decoder: Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
        }
    }

    struct Child: Decodable, Equatable, Sendable {
        var id: String
        var name: String
    }

    var id: String
    var kind: Kind
    var child: Child
    var title: String
    var points: Int?
    var submittedAt: Date
    var minutesRequested: Int?
}

/// `GET /api/inbox` → `{ items }`, oldest first; every item is pending.
struct InboxResponse: Decodable, Sendable {
    var items: [InboxItem]
}

/// `POST /api/inbox/{id}/reject` `{ reason? }`. An empty reason is omitted (the server refuses one).
struct RejectRequest: Encodable, Equatable, Sendable {
    var reason: String?

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(reason.flatMap { $0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : $0 }, forKey: .reason)
    }

    private enum CodingKeys: String, CodingKey { case reason }
}

/// `{ id, state }` after a decision.
struct Decision: Decodable, Equatable, Sendable {
    enum State: String, Decodable, Sendable { case approved, rejected }
    var id: String
    var state: State
}

enum PushEnvironment: String, Encodable, Sendable {
    case development, production

    /// The APNs environment this install's token belongs to, from the
    /// `aps-environment` entitlement in the embedded provisioning profile. App Store
    /// and TestFlight installs have no embedded profile and are always production.
    static let current: PushEnvironment = from(provisioningProfile: Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision")
        .flatMap { try? Data(contentsOf: $0) })

    /// `nil` (no profile) is production. A profile without a readable
    /// `aps-environment` is assumed to be development: Xcode and ad-hoc profiles.
    static func from(provisioningProfile data: Data?) -> PushEnvironment {
        guard let data else { return .production }
        guard let entitlements = ProvisioningProfile.entitlements(in: data),
              let value = entitlements["aps-environment"] as? String else { return .development }
        return value == "production" ? .production : .development
    }
}

/// Reads the property list inside a CMS-signed `embedded.mobileprovision` without
/// verifying the signature (the system already did, at install).
enum ProvisioningProfile {
    static func entitlements(in data: Data) -> [String: Any]? {
        guard let start = data.range(of: Data("<?xml".utf8)),
              let end = data.range(of: Data("</plist>".utf8), in: start.lowerBound..<data.endIndex) else { return nil }
        let plist = data.subdata(in: start.lowerBound..<end.upperBound)
        let root = try? PropertyListSerialization.propertyList(from: plist, format: nil) as? [String: Any]
        return root?["Entitlements"] as? [String: Any]
    }
}

struct PushTokenRequest: Encodable, Equatable, Sendable {
    var token: String
    var environment: PushEnvironment
}

struct PushTokenResponse: Decodable {}

/// `POST /api/v1/parent/passkey/options` → the WebAuthn request options we need.
struct PasskeyOptionsResponse: Decodable, Sendable {
    struct Options: Decodable, Sendable {
        /// base64url
        var challenge: String
        var rpId: String
    }
    var challengeId: String
    var options: Options
}

/// WebAuthn `AuthenticationResponseJSON`, every binary value base64url without padding.
struct AuthenticationResponseJSON: Encodable, Equatable, Sendable {
    struct Response: Encodable, Equatable, Sendable {
        var clientDataJSON: String
        var authenticatorData: String
        var signature: String
        var userHandle: String?
    }
    struct Extensions: Encodable, Equatable, Sendable {}

    var id: String
    var rawId: String
    var type = "public-key"
    var response: Response
    var clientExtensionResults = Extensions()

    init(_ assertion: PasskeyAssertion) {
        id = assertion.credentialId
        rawId = assertion.credentialId
        response = Response(clientDataJSON: assertion.clientDataJSON, authenticatorData: assertion.authenticatorData,
                            signature: assertion.signature, userHandle: assertion.userHandle)
    }
}

struct PasskeySessionRequest: Encodable, Equatable, Sendable {
    var challengeId: String
    var credential: AuthenticationResponseJSON
    var deviceName: String
}


protocol ParentAPI: Sendable {
    func signIn(_ request: SignInRequest) async throws -> SignInResponse
    func passkeyOptions() async throws -> PasskeyOptionsResponse
    func signIn(passkey request: PasskeySessionRequest) async throws -> SignInResponse
    func session() async throws -> SessionResponse
    func signOut() async throws
    func inbox() async throws -> InboxResponse
    /// `idempotencyKey` makes a retry replay the first answer instead of deciding twice.
    func approve(id: String, idempotencyKey: String) async throws -> Decision
    func reject(id: String, _ request: RejectRequest, idempotencyKey: String) async throws -> Decision
    func registerPushToken(_ request: PushTokenRequest) async throws
    func unregisterPushToken() async throws
    /// Answers a child's "Ask for more time".
    func answerTimeRequest(id: String, _ request: TimeAnswerRequest) async throws
}

extension ParentAPI {
    func answerTimeRequest(id: String, _ request: TimeAnswerRequest) async throws { throw APIError.notConfigured }
}

/// `POST /api/v1/parent/time-requests/{id}/answer`.
struct TimeAnswerRequest: Encodable, Equatable, Sendable {
    var requestId: String
    var expectedVersion: Int
    var choice: String
}
struct TimeAnswerResponse: Decodable, Sendable {}

struct HTTPParentAPI: ParentAPI {
    let baseURL: URL
    let token: String?
    var session: URLSession = .shared

    private var http: APITransport { APITransport(baseURL: baseURL, token: token, session: session) }

    func signIn(_ request: SignInRequest) async throws -> SignInResponse {
        try await APITransport(baseURL: baseURL, token: nil, session: session)
            .post("v1/parent/sessions", body: request, as: SignInResponse.self)
    }

    func passkeyOptions() async throws -> PasskeyOptionsResponse {
        try await APITransport(baseURL: baseURL, token: nil, session: session)
            .post("v1/parent/passkey/options", body: EmptyRequest(), as: PasskeyOptionsResponse.self)
    }

    func signIn(passkey request: PasskeySessionRequest) async throws -> SignInResponse {
        try await APITransport(baseURL: baseURL, token: nil, session: session)
            .post("v1/parent/passkey/sessions", body: request, as: SignInResponse.self)
    }

    func session() async throws -> SessionResponse {
        try await http.get("v1/parent/session", as: SessionResponse.self)
    }

    func signOut() async throws {
        try await http.delete("v1/parent/session")
    }

    func inbox() async throws -> InboxResponse {
        try await http.get("inbox", as: InboxResponse.self)
    }

    func approve(id: String, idempotencyKey: String) async throws -> Decision {
        try await http.post("inbox/\(InboxPath.id(id))/approve", body: EmptyRequest(), headers: ["Idempotency-Key": idempotencyKey], as: Decision.self)
    }

    func reject(id: String, _ request: RejectRequest, idempotencyKey: String) async throws -> Decision {
        try await http.post("inbox/\(InboxPath.id(id))/reject", body: request, headers: ["Idempotency-Key": idempotencyKey], as: Decision.self)
    }

    func answerTimeRequest(id: String, _ request: TimeAnswerRequest) async throws {
        _ = try await http.post("v1/parent/time-requests/\(id)/answer", body: request, as: TimeAnswerResponse.self)
    }

    func registerPushToken(_ request: PushTokenRequest) async throws {
        _ = try await http.put("v1/parent/push-token", body: request, as: PushTokenResponse.self)
    }

    func unregisterPushToken() async throws {
        try await http.delete("v1/parent/push-token")
    }
}

import Foundation

// Parent device API (api/src/phones.yaml, inbox.yaml). The phone signs in with a password, a
// passkey, or the web view's sign-in, and gets an `ocp_` phone session that acts as its adult:
// the inbox, decisions and its APNs token.

/// An inbox id as a path segment: ids are UUIDs, anything else is refused rather
/// than put in a URL.
enum InboxPath {
    static func id(_ id: String) -> String {
        UUID(uuidString: id) != nil ? id.lowercased() : "invalid"
    }
}

/// `PhoneSession`: this phone's session as Manage lists it.
struct ParentSessionInfo: Decodable, Equatable, Sendable {
    var id: String
    /// What the phone is called in Manage.
    var name: String
    var createdAt: Date
    var expiresAt: Date
    var lastUsedAt: Date?
}

struct HouseholdInfo: Decodable, Equatable, Sendable {
    var id: String
    var name: String
}

/// `Me`: the adult and their household.
struct ParentMe: Decodable, Equatable, Sendable {
    struct Adult: Decodable, Equatable, Sendable {
        var id: String
        var name: String
    }
    var adult: Adult
    var household: HouseholdInfo
}

struct SignInRequest: Encodable, Sendable {
    var email: String
    var password: String
    var deviceName: String
}

/// `PhoneSignedIn`, from every sign-in.
struct SignInResponse: Decodable, Sendable {
    var token: String
    var session: ParentSessionInfo
    var me: ParentMe
}

/// `PhoneSelf`: `GET /api/phone/session`.
struct SessionResponse: Decodable, Sendable {
    var session: ParentSessionInfo
    var me: ParentMe
    /// While in the future, this adult's phones get no alerts (silent updates still arrive).
    var pushMutedUntil: Date?
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
        try c.encodeIfPresent(sentReason, forKey: .reason)
    }

    /// The reason as sent: nil when empty or only whitespace.
    var sentReason: String? {
        reason.flatMap { $0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : $0 }
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

/// `PUT /api/phone/session/push` (and the kid's `PUT /api/device/push`).
struct PushTokenRequest: Encodable, Equatable, Sendable {
    var token: String
    var environment: PushEnvironment
}

struct PushTokenResponse: Decodable {}

/// `POST /api/auth/passkey/options` → the WebAuthn request options we need.
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
    /// Answers a child's "Ask for more time" (an inbox item of kind `time_request`).
    func answer(id: String, _ request: TimeAnswerRequest, idempotencyKey: String) async throws -> Decision
    func registerPushToken(_ request: PushTokenRequest) async throws
    func unregisterPushToken() async throws
}

/// `POST /api/inbox/{id}/answer` `{ choice }`: `30min`, `1h`, `all_day` or `no`.
struct TimeAnswerRequest: Encodable, Equatable, Sendable {
    var choice: String
}

struct HTTPParentAPI: ParentAPI {
    let baseURL: URL
    let token: String?
    var session: URLSession = .shared

    private var http: APITransport { APITransport(baseURL: baseURL, token: token, session: session) }

    func signIn(_ request: SignInRequest) async throws -> SignInResponse {
        try await APITransport(baseURL: baseURL, token: nil, session: session)
            .post("phone/sessions", body: request, as: SignInResponse.self)
    }

    func passkeyOptions() async throws -> PasskeyOptionsResponse {
        try await APITransport(baseURL: baseURL, token: nil, session: session)
            .post("auth/passkey/options", body: EmptyRequest(), as: PasskeyOptionsResponse.self)
    }

    func signIn(passkey request: PasskeySessionRequest) async throws -> SignInResponse {
        try await APITransport(baseURL: baseURL, token: nil, session: session)
            .post("phone/sessions/passkey", body: request, as: SignInResponse.self)
    }

    func session() async throws -> SessionResponse {
        try await http.get("phone/session", as: SessionResponse.self)
    }

    func signOut() async throws {
        try await http.delete("phone/session")
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

    func answer(id: String, _ request: TimeAnswerRequest, idempotencyKey: String) async throws -> Decision {
        try await http.post("inbox/\(InboxPath.id(id))/answer", body: request, headers: ["Idempotency-Key": idempotencyKey], as: Decision.self)
    }

    /// Commands need an Idempotency-Key; registering the same token again is harmless, so each
    /// call gets a fresh one.
    func registerPushToken(_ request: PushTokenRequest) async throws {
        _ = try await http.put("phone/session/push", body: request, headers: ["Idempotency-Key": UUID().uuidString.lowercased()], as: PushTokenResponse.self)
    }

    func unregisterPushToken() async throws {
        _ = try await http.send("DELETE", "phone/session/push", headers: ["Idempotency-Key": UUID().uuidString.lowercased()])
    }
}

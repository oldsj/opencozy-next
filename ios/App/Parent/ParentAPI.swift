import Foundation

// Parent device API (contract "Phase 2 server API"). A parent session token
// (`ocp_…`) only works on `/api/v1/parent/*`. These are still the previous server's
// paths: they move to this server's API with slices 10 (passkeys) and 11 (phone
// sessions, pushes).

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

struct Approval: Decodable, Equatable, Identifiable, Sendable {
    enum State: String, Decodable, Sendable {
        case pending, approved, rejected, withdrawn, unknown
        init(from decoder: Decoder) throws {
            self = State(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
        }
    }

    enum Kind: String, Decodable, Sendable {
        case chore, routine, unknown
        init(from decoder: Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
        }
    }

    var id: String
    var version: Int
    var state: State
    var kind: Kind
    var childId: String
    var childName: String
    var title: String
    var points: Int
    var localDay: String
    var requestedAt: Date
    var decidedAt: Date?
    var reason: String?
}

struct ApprovalsResponse: Decodable, Sendable {
    var approvals: [Approval]
    var pendingCount: Int
}

struct ApproveRequest: Encodable, Equatable, Sendable {
    var requestId: String
    var expectedVersion: Int
}

struct RejectRequest: Encodable, Equatable, Sendable {
    var requestId: String
    var expectedVersion: Int
    /// Omitted when empty: the server rejects an empty reason.
    var reason: String?

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(requestId, forKey: .requestId)
        try c.encode(expectedVersion, forKey: .expectedVersion)
        try c.encodeIfPresent(reason, forKey: .reason)
    }

    private enum CodingKeys: String, CodingKey { case requestId, expectedVersion, reason }
}

struct DecisionResponse: Decodable, Sendable {
    var approval: Approval
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
    func approvals() async throws -> ApprovalsResponse
    func approve(id: String, _ request: ApproveRequest) async throws -> Approval
    func reject(id: String, _ request: RejectRequest) async throws -> Approval
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

    func approvals() async throws -> ApprovalsResponse {
        try await http.get("v1/parent/approvals", as: ApprovalsResponse.self)
    }

    func approve(id: String, _ request: ApproveRequest) async throws -> Approval {
        try await http.post("v1/parent/approvals/\(id)/approve", body: request, as: DecisionResponse.self).approval
    }

    func reject(id: String, _ request: RejectRequest) async throws -> Approval {
        try await http.post("v1/parent/approvals/\(id)/reject", body: request, as: DecisionResponse.self).approval
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

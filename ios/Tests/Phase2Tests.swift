import CryptoKit
import Foundation
import Testing
@testable import OpenCozy

/// The server decides which child a device belongs to; a reassignment re-renders
/// for the new child and nothing of the old child's carries over.
@MainActor
struct DeviceReassignmentTests {
    actor Owner {
        var childId = FixtureAPI.childId
        func set(_ id: String) { childId = id }
    }

    struct ReassignableAPI: OpenCozyAPI {
        let owner: Owner
        let unlockedMinutes: Int?
        func device() async throws -> DeviceInfo {
            var info = try await FixtureAPI().device()
            let id = await owner.childId
            info.device.child = DeviceChild(id: id, name: id == FixtureAPI.childId ? "Robin" : "Sky", color: nil)
            return info
        }
        func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
            // Only the original child has time running.
            let (response, at) = try await FixtureAPI(unlockedMinutes: unlockedMinutes).screenTime(target: target)
            return (response, at)
        }
    }

    @Test func followsTheServersChild() async {
        let owner = Owner()
        let model = AppModel(preview: ReassignableAPI(owner: owner, unlockedMinutes: 10))
        await model.refresh()
        #expect(model.childName == "Robin")
        #expect(model.unlockedUntil != nil)
        await owner.set("00000000-0000-4000-8000-0000000000c2")
        await model.refresh()
        #expect(model.childId == "00000000-0000-4000-8000-0000000000c2")
        #expect(model.childName == "Sky")
        // Robin's window isn't Sky's.
        #expect(model.unlockedUntil == nil)
    }
}

// MARK: Parent

struct ApprovalPushTests {
    @Test func pendingPayload() throws {
        let info: [AnyHashable: Any] = [
            "aps": ["badge": 3, "category": "APPROVAL"],
            "opencozy": ["type": "approval.pending", "approvalId": "a1", "approvalVersion": 1, "pendingCount": 3],
        ]
        #expect(ApprovalPush(userInfo: info) == ApprovalPush(approvalId: "a1", event: .pending(version: 1), pendingCount: 3))
    }

    @Test func resolvedPayload() {
        let info: [AnyHashable: Any] = ["opencozy": ["type": "approval.resolved", "approvalId": "a1", "state": "approved", "pendingCount": 2]]
        #expect(ApprovalPush(userInfo: info) == ApprovalPush(approvalId: "a1", event: .resolved, pendingCount: 2))
    }

    @Test func rejectsIncompletePayloads() {
        #expect(ApprovalPush(userInfo: [:]) == nil)
        #expect(ApprovalPush(userInfo: ["opencozy": ["type": "approval.pending", "approvalId": "a1"]]) == nil)
        #expect(ApprovalPush(userInfo: ["opencozy": ["type": "other", "approvalId": "a1"]]) == nil)
    }

    @Test func actionIdentifiersMatchTheContract() {
        #expect(ApprovalDecision(rawValue: "APPROVE") == .approve)
        #expect(ApprovalDecision(rawValue: "REJECT") == .reject)
        #expect(ApprovalNotifications.category == "APPROVAL")
    }

    @Test func deviceTokenIsLowercaseHex() {
        #expect(Data([0x0a, 0xff, 0x00]).hexString == "0aff00")
    }
}

struct ParentWireTests {
    @Test func rejectOmitsEmptyReason() throws {
        let without = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RejectRequest(requestId: "r", expectedVersion: 1, reason: nil))) as? [String: Any]
        #expect(without?.keys.sorted() == ["expectedVersion", "requestId"])
        let with = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RejectRequest(requestId: "r", expectedVersion: 1, reason: "Not yet"))) as? [String: Any]
        #expect(with?["reason"] as? String == "Not yet")
    }

    @Test func decodesApprovalList() throws {
        let json = """
        { "approvals": [ { "id": "a", "version": 1, "state": "pending", "kind": "routine", "childId": "k", "childName": "Ada",
          "title": "Bedtime", "points": 8, "localDay": "2026-11-07", "requestedAt": "2026-11-07T10:00:00.000Z", "decidedAt": null,
          "reason": null, "choreOccurrenceId": null, "routineOccurrenceId": "r" } ], "pendingCount": 1 }
        """
        let result = try APIDates.decoder().decode(ApprovalsResponse.self, from: Data(json.utf8))
        #expect(result.approvals.first?.kind == .routine)
        #expect(result.pendingCount == 1)
    }

    @Test func pushEnvironmentEncodesAsContractValue() throws {
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(PushTokenRequest(token: "ab", environment: .development))) as? [String: Any]
        #expect(body?["environment"] as? String == "development")
    }
}

actor FakeParentServer {
    var approvals: [Approval]
    var approves: [(String, ApproveRequest)] = []
    var rejects: [(String, RejectRequest)] = []
    var pushTokens: [PushTokenRequest] = []
    var signedOut = false
    var failures: [APIError] = []
    var listFailure: APIError?

    init(approvals: [Approval]) { self.approvals = approvals }

    func fail(_ errors: [APIError]) { failures = errors }
    func failList(_ error: APIError?) { listFailure = error }

    func list() throws -> ApprovalsResponse {
        if let listFailure { throw listFailure }
        return ApprovalsResponse(approvals: approvals, pendingCount: approvals.filter { $0.state == .pending }.count)
    }

    func approve(_ id: String, _ r: ApproveRequest) throws -> Approval {
        approves.append((id, r))
        if !failures.isEmpty { throw failures.removeFirst() }
        return try decide(id, .approved)
    }

    func reject(_ id: String, _ r: RejectRequest) throws -> Approval {
        rejects.append((id, r))
        if !failures.isEmpty { throw failures.removeFirst() }
        return try decide(id, .rejected)
    }

    private func decide(_ id: String, _ state: Approval.State) throws -> Approval {
        guard let index = approvals.firstIndex(where: { $0.id == id }) else { throw APIError.server(status: 404, message: "Approval not found.") }
        approvals[index].state = state
        approvals[index].version = 2
        return approvals[index]
    }

    var passkeyRequests: [PasskeySessionRequest] = []
    func recordPasskey(_ r: PasskeySessionRequest) { passkeyRequests.append(r) }
    func register(_ r: PushTokenRequest) { pushTokens.append(r) }
    func signOut() { signedOut = true }
}

struct FakeParentAPI: ParentAPI {
    let server: FakeParentServer
    let token: String?

    func signIn(_ request: SignInRequest) async throws -> SignInResponse {
        guard request.password == "correct horse" else { throw APIError.server(status: 401, message: "Invalid email or password.") }
        return SignInResponse(token: "ocp_test", session: Self.session, household: HouseholdInfo(id: "h", name: "The Example House"))
    }
    func passkeyOptions() async throws -> PasskeyOptionsResponse {
        PasskeyOptionsResponse(challengeId: "ch1", options: .init(challenge: Data("challenge-bytes".utf8).base64URL, rpId: "opencozy.example.org"))
    }
    func signIn(passkey request: PasskeySessionRequest) async throws -> SignInResponse {
        await server.recordPasskey(request)
        guard request.challengeId == "ch1", request.credential.response.signature == "c2ln" else {
            throw APIError.server(status: 401, message: "Passkey not recognized.")
        }
        return SignInResponse(token: "ocp_passkey", session: Self.session, household: HouseholdInfo(id: "h", name: "The Example House"))
    }
    func session() async throws -> SessionResponse { SessionResponse(session: Self.session, household: HouseholdInfo(id: "h", name: "The Example House")) }
    func signOut() async throws { await server.signOut() }
    func approvals() async throws -> ApprovalsResponse { try await server.list() }
    func approve(id: String, _ request: ApproveRequest) async throws -> Approval { try await server.approve(id, request) }
    func reject(id: String, _ request: RejectRequest) async throws -> Approval { try await server.reject(id, request) }
    func registerPushToken(_ request: PushTokenRequest) async throws { await server.register(request) }
    func unregisterPushToken() async throws {}

    static let session = ParentSessionInfo(id: "s", deviceName: "Test phone", createdAt: Date(), expiresAt: Date().addingTimeInterval(90 * 86_400), lastUsedAt: nil)
}

@MainActor
struct ParentModelTests {
    static func approval(_ id: String, minutesAgo: Double, state: Approval.State = .pending) -> Approval {
        Approval(id: id, version: 1, state: state, kind: .chore, childId: "k", childName: "Ada", title: "Chore \(id)", points: 5,
                 localDay: "2026-11-07", requestedAt: Date().addingTimeInterval(-minutesAgo * 60), decidedAt: nil, reason: nil)
    }

    private func signedIn(_ approvals: [Approval]) async -> (ParentModel, FakeParentServer) {
        let server = FakeParentServer(approvals: approvals)
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, token in FakeParentAPI(server: server, token: token) }, notifier: .none)
        let ok = await model.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Test phone")
        #expect(ok)
        return (model, server)
    }

    @Test func signInStoresTokenAndLoadsInboxOldestFirst() async {
        let (model, _) = await signedIn([Self.approval("new", minutesAgo: 1), Self.approval("old", minutesAgo: 30),
                                         Self.approval("done", minutesAgo: 60, state: .approved)])
        #expect(model.store.token == "ocp_test")
        #expect(model.approvals.map(\.id) == ["old", "new"])
        #expect(model.household?.name == "The Example House")
    }

    @Test func wrongPasswordShowsServerMessageAndStoresNothing() async {
        let server = FakeParentServer(approvals: [])
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, t in FakeParentAPI(server: server, token: t) }, notifier: .none)
        let ok = await model.signIn(server: "https://opencozy.example.org", email: "p@example.org", password: "nope", deviceName: "x")
        #expect(!ok)
        #expect(model.errorMessage == "Invalid email or password.")
        #expect(store.token == nil)
    }

    @Test func approveSendsVersionAndRemovesItem() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        #expect(await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending(version: 1))) == .done)
        let (id, request) = try #require(await server.approves.first)
        #expect(id == "a")
        #expect(request.expectedVersion == 1)
        #expect(model.approvals.isEmpty)
    }

    @Test func rejectFromANotificationHasNoReason() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        #expect(await model.handleAction(.reject, push: ApprovalPush(approvalId: "a", event: .pending(version: 1))) == .done)
        #expect(await server.rejects.map(\.1.reason) == [nil])
    }

    @Test func conflictFromAnotherParentRemovesTheItem() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        // Meanwhile the other parent approved it, so the next list is empty.
        _ = try await server.approve("a", ApproveRequest(requestId: "other", expectedVersion: 1))
        await server.fail([.server(status: 409, message: "Approval was already approved.")])
        #expect(await model.handleAction(.reject, push: ApprovalPush(approvalId: "a", event: .pending(version: 1))) == .alreadyHandled)
        #expect(model.approvals.isEmpty)
        #expect(model.errorMessage == "Approval was already approved.")
    }

    @Test func unauthorizedSignsOut() async {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        await server.failList(.server(status: 401, message: "Unauthorized."))
        await model.refresh()
        #expect(!model.isSignedIn)
        #expect(model.store.token == nil)
        #expect(model.approvals.isEmpty)
    }

    /// Review S5: an adult revoked this phone in Manage → Devices. The web view
    /// still holds the adult cookie, but the app must not quietly mint a new
    /// session, even after a relaunch, until a manual sign-in from the bell sheet.
    @Test func revokeSticksAcrossRelaunchUntilManualSignIn() async {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        await server.failList(.server(status: 401, message: "Unauthorized."))
        await model.refresh()
        #expect(!model.isSignedIn)
        #expect(model.store.webHandoffOff)

        // Relaunch: a new model on the same storage.
        let relaunched = ParentModel(store: model.store, makeAPI: { _, token in FakeParentAPI(server: server, token: token) }, notifier: .none)
        var minted = false
        let result = await relaunched.enableFromWebSession(deviceName: "Phone") { _ in minted = true; return (201, Data()) }
        #expect(result == .skipped)
        #expect(!minted)
        #expect(!relaunched.isSignedIn)

        // Manual sign-in from the bell sheet turns the automatic path back on.
        await server.failList(nil)
        #expect(await relaunched.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Phone"))
        #expect(!relaunched.store.webHandoffOff)
    }

    @Test func offlineSaysHomeOrVPN() async {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        await server.failList(.transport("The request timed out."))
        await model.refresh()
        #expect(model.isUnreachable)
        #expect(model.errorMessage == ParentModel.unreachableMessage)
        // The last good inbox stays visible.
        #expect(model.approvals.count == 1)
    }

    @Test func notificationActionRetriesWithTheSameRequestId() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        await server.fail([.transport("offline")])
        let ok = await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending(version: 1)))
        #expect(ok == .done)
        let approves = await server.approves
        #expect(approves.count == 2)
        #expect(approves[0].1 == approves[1].1)
        #expect(approves[0].1.expectedVersion == 1)
    }

    @Test func resolvedPushIsNotAnAction() async {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        let ok = await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .resolved))
        #expect(ok == .failed)
        #expect(await server.approves.isEmpty)
    }

    @Test func pushTokenRegisteredOncePerToken() async {
        let (model, server) = await signedIn([])
        let token = Data([0xab, 0xcd])
        await model.didReceivePushToken(token)
        await model.didReceivePushToken(token)
        let tokens = await server.pushTokens
        #expect(tokens.map(\.token) == ["abcd"])
    }

    @Test func signOutClearsEverything() async {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        await model.signOut()
        #expect(await server.signedOut)
        #expect(!model.isSignedIn)
        #expect(model.approvals.isEmpty)
    }
}

// MARK: Device key

struct DeviceKeyTests {
    @Test func signatureVerifiesAgainstPublishedKey() throws {
        let key = SoftwareDeviceKey(key: P256.Signing.PrivateKey())
        let message = Data("GET\n/api/v1/children\n".utf8)
        let publicKey = try P256.Signing.PublicKey(x963Representation: key.publicKeyX963)
        #expect(try publicKey.isValidSignature(P256.Signing.ECDSASignature(derRepresentation: key.signatureDER(for: message)), for: message))
        #expect(try publicKey.isValidSignature(P256.Signing.ECDSASignature(rawRepresentation: key.signatureRaw(for: message)), for: message))
        #expect(key.publicKeyX963.count == 65)
        #expect(try P256.Signing.PublicKey(derRepresentation: key.publicKeySPKI) == publicKey)
    }

    @Test func base64URLRoundTrip() {
        let bytes = Data([0xfb, 0xff, 0xfe, 0x00, 0x01])
        #expect(!bytes.base64URL.contains("+") && !bytes.base64URL.contains("/") && !bytes.base64URL.contains("="))
        #expect(Data(base64URL: bytes.base64URL) == bytes)
    }
}

extension P256.Signing.PublicKey: @retroactive Equatable {
    public static func == (a: Self, b: Self) -> Bool { a.x963Representation == b.x963Representation }
}

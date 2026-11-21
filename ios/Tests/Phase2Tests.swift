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
        func screenTime(childId: String) async throws -> (ScreenTime, receivedAt: Date) {
            // Only the original child has time running.
            let (response, at) = try await FixtureAPI(unlockedMinutes: unlockedMinutes).screenTime(childId: childId)
            return (response, at)
        }
    }

    @Test func followsTheServersChild() async {
        let owner = Owner()
        let model = AppModel(preview: ReassignableAPI(owner: owner, unlockedMinutes: 10))
        await model.refresh()
        #expect(model.childName == "Robin")
        #expect(model.unlockedUntil != nil)
        // Robin pressed these on the shield just before the move: they aren't Sky's to send.
        model.lock.store.enqueue(ScreenTimeEvent(id: "00000000-0000-4000-8000-0000000000e9", kind: .oneMoreMinute, at: .now()))
        model.lock.store.queuedTimeRequest = QueuedTimeRequest(requestId: "00000000-0000-4000-8000-0000000000f9", at: Date())
        await owner.set("00000000-0000-4000-8000-0000000000c2")
        await model.refresh()
        #expect(model.lock.store.pendingEvents.isEmpty)
        #expect(model.lock.store.queuedTimeRequest == nil)
        #expect(model.childId == "00000000-0000-4000-8000-0000000000c2")
        #expect(model.childName == "Sky")
        // Robin's window isn't Sky's.
        #expect(model.unlockedUntil == nil)
    }
}

// MARK: Parent

/// This server answers the previous server's parent paths 410 `update_app`: parent mode on
/// the phone isn't here yet, which isn't the same as the app being out of date.
@MainActor
struct ParentModeLaterTests {
    struct Gone: ParentAPI {
        static let gone = APIError.server(status: 410, message: "This app is out of date. Update OpenCozy on this device.")
        func signIn(_ request: SignInRequest) async throws -> SignInResponse { throw Self.gone }
        func passkeyOptions() async throws -> PasskeyOptionsResponse { throw Self.gone }
        func signIn(passkey request: PasskeySessionRequest) async throws -> SignInResponse { throw Self.gone }
        func session() async throws -> SessionResponse { throw Self.gone }
        func signOut() async throws { throw Self.gone }
        func inbox() async throws -> InboxResponse { throw Self.gone }
        func approve(id: String, idempotencyKey: String) async throws -> Decision { throw Self.gone }
        func reject(id: String, _ request: RejectRequest, idempotencyKey: String) async throws -> Decision { throw Self.gone }
        func answer(id: String, _ request: TimeAnswerRequest, idempotencyKey: String) async throws -> Decision { throw Self.gone }
        func registerPushToken(_ request: PushTokenRequest) async throws { throw Self.gone }
        func unregisterPushToken() async throws { throw Self.gone }
    }

    @Test func signInSaysParentModeComesLater() async {
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, _ in Gone() }, notifier: .none)
        let ok = await model.signIn(server: "https://opencozy.example.org", email: "adult@example.test", password: "x", deviceName: "Phone")
        #expect(!ok)
        #expect(model.errorMessage == ParentModel.parentModeLater)
    }
}

struct ApprovalPushTests {
    @Test func pendingPayload() throws {
        let info: [AnyHashable: Any] = [
            "aps": ["badge": 3, "category": "APPROVAL"],
            "opencozy": ["type": "approval.pending", "itemId": "a1", "kind": "completion", "pendingCount": 3],
        ]
        #expect(ApprovalPush(userInfo: info) == ApprovalPush(approvalId: "a1", event: .pending, kind: "completion", pendingCount: 3))
        #expect(ApprovalPush(userInfo: info)?.isTimeRequest == false)
    }

    @Test func timeRequestPayload() {
        let info: [AnyHashable: Any] = ["opencozy": ["type": "approval.pending", "itemId": "t1", "kind": "time_request", "pendingCount": 1]]
        #expect(ApprovalPush(userInfo: info)?.isTimeRequest == true)
        #expect(TimeChoice.allCases.map(\.wire) == ["30min", "1h", "all_day", "no"])
        #expect(ApprovalNotifications.timeCategory == "TIME_REQUEST")
    }

    @Test func resolvedPayload() {
        let info: [AnyHashable: Any] = ["opencozy": ["type": "approval.resolved", "itemId": "a1", "state": "approved", "pendingCount": 2]]
        #expect(ApprovalPush(userInfo: info) == ApprovalPush(approvalId: "a1", event: .resolved, pendingCount: 2))
    }

    @Test func rejectsIncompletePayloads() {
        #expect(ApprovalPush(userInfo: [:]) == nil)
        #expect(ApprovalPush(userInfo: ["opencozy": ["type": "approval.pending", "approvalId": "a1"]]) == nil)
        #expect(ApprovalPush(userInfo: ["opencozy": ["type": "other", "itemId": "a1"]]) == nil)
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
        let without = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RejectRequest(reason: nil))) as? [String: Any]
        #expect(without?.isEmpty == true)
        let blank = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RejectRequest(reason: "  "))) as? [String: Any]
        #expect(blank?.isEmpty == true)
        let with = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RejectRequest(reason: "Not yet"))) as? [String: Any]
        #expect(with?["reason"] as? String == "Not yet")
    }

    @Test func decodesInbox() throws {
        let json = """
        { "items": [
          { "id": "0b9e2f4a-6c1d-4e8b-9a37-5f2d8c1e6b40", "kind": "routine", "child": { "id": "c1", "name": "Robin", "color": "sky" },
            "title": "Bedtime", "points": 8, "submittedAt": "2026-11-07T10:00:00.000Z" },
          { "id": "5e1c7a92-3b4d-4f60-8e21-9c0a6d3f7b18", "kind": "time_request", "child": { "id": "c1", "name": "Robin", "color": "sky" },
            "title": "More time", "submittedAt": "2026-11-07T10:05:00Z", "minutesRequested": 30 },
          { "id": "a3f0c6d8-1e2b-4c79-b5a4-7d8e9f0a1b2c", "kind": "something_new", "child": { "id": "c2", "name": "Sam" },
            "title": "Later kind", "submittedAt": "2026-11-07T10:06:00Z" } ] }
        """
        let result = try APIDates.decoder().decode(InboxResponse.self, from: Data(json.utf8))
        #expect(result.items.map(\.kind) == [.routine, .timeRequest, .unknown])
        #expect(result.items[1].minutesRequested == 30)
        #expect(result.items[1].points == nil)
    }

    @Test func decodesDecision() throws {
        let decision = try APIDates.decoder().decode(Decision.self, from: Data(#"{"id":"x","state":"rejected"}"#.utf8))
        #expect(decision == Decision(id: "x", state: .rejected))
    }

    @Test func idempotencyKeyIsPerSessionItemAndDecision() {
        let session = "7C2D9E10-4A3B-4C5D-8E6F-0A1B2C3D4E5F", id = "0B9E2F4A-6C1D-4E8B-9A37-5F2D8C1E6B40"
        let approve = ParentModel.idempotencyKey(session: session, id, .approve)
        #expect(approve == "7c2d9e10-4a3b-4c5d-8e6f-0a1b2c3d4e5f:0b9e2f4a-6c1d-4e8b-9a37-5f2d8c1e6b40:approve")
        #expect(ParentModel.idempotencyKey(session: session, id, .reject).hasSuffix(":reject"))
        // Another parent's phone deciding the same item uses a different key.
        #expect(ParentModel.idempotencyKey(session: "0d4c3b2a-1f0e-4d9c-8b7a-6f5e4d3c2b1a", id, .approve) != approve)
        // A reject's reason, as sent, is part of the key: an edited reason is a new key.
        let plain = ParentModel.idempotencyKey(session: session, id, .reject)
        #expect(ParentModel.idempotencyKey(session: session, id, .reject, reason: "  ") == plain)
        let notYet = ParentModel.idempotencyKey(session: session, id, .reject, reason: "Not yet")
        #expect(notYet.hasPrefix(plain + ":") && notYet.count == plain.count + 13)
        #expect(ParentModel.idempotencyKey(session: session, id, .reject, reason: "Not yet") == notYet)
        #expect(ParentModel.idempotencyKey(session: session, id, .reject, reason: "Not today") != notYet)
        #expect(ParentModel.idempotencyKey(session: session, id, .approve, reason: "ignored") == approve)
        // The contract allows 1-128 printable ASCII characters.
        #expect(notYet.count <= 128)
        #expect(approve.count <= 128)
        #expect(approve.allSatisfy { $0.isASCII && !$0.isWhitespace })
    }

    @Test func inboxPathOnlyTakesUUIDs() {
        #expect(InboxPath.id("0B9E2F4A-6C1D-4E8B-9A37-5F2D8C1E6B40") == "0b9e2f4a-6c1d-4e8b-9a37-5f2d8c1e6b40")
        #expect(InboxPath.id("../devices") == "invalid")
    }

    @Test func pushEnvironmentEncodesAsContractValue() throws {
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(PushTokenRequest(token: "ab", environment: .development))) as? [String: Any]
        #expect(body?["environment"] as? String == "development")
    }
}

actor FakeParentServer {
    var approvals: [InboxItem]
    var approves: [(id: String, key: String)] = []
    var rejects: [(id: String, request: RejectRequest, key: String)] = []
    var answers: [(id: String, choice: String, key: String)] = []
    var pushTokens: [PushTokenRequest] = []
    var signedOut = false
    var failures: [APIError] = []
    var listFailure: APIError?

    init(approvals: [InboxItem]) { self.approvals = approvals }

    func fail(_ errors: [APIError]) { failures = errors }
    func failList(_ error: APIError?) { listFailure = error }

    func list() throws -> InboxResponse {
        if let listFailure { throw listFailure }
        return InboxResponse(items: approvals)
    }

    func approve(_ id: String, key: String) throws -> Decision {
        approves.append((id, key))
        if !failures.isEmpty { throw failures.removeFirst() }
        return try decide(id, .approved)
    }

    func reject(_ id: String, _ r: RejectRequest, key: String) throws -> Decision {
        rejects.append((id, r, key))
        if !failures.isEmpty { throw failures.removeFirst() }
        return try decide(id, .rejected)
    }

    func answer(_ id: String, _ choice: String, key: String) throws -> Decision {
        answers.append((id, choice, key))
        if !failures.isEmpty { throw failures.removeFirst() }
        return try decide(id, choice == "no" ? .rejected : .approved)
    }

    private func decide(_ id: String, _ state: Decision.State) throws -> Decision {
        guard approvals.contains(where: { $0.id == id }) else {
            throw APIError.server(status: 409, message: "Someone already decided this one.")
        }
        approvals.removeAll { $0.id == id }
        return Decision(id: id, state: state)
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
        return SignInResponse(token: "ocp_test", session: Self.session, me: Self.me)
    }
    func passkeyOptions() async throws -> PasskeyOptionsResponse {
        PasskeyOptionsResponse(challengeId: "ch1", options: .init(challenge: Data("challenge-bytes".utf8).base64URL, rpId: "opencozy.example.org"))
    }
    func signIn(passkey request: PasskeySessionRequest) async throws -> SignInResponse {
        await server.recordPasskey(request)
        guard request.challengeId == "ch1", request.credential.response.signature == "c2ln" else {
            throw APIError.server(status: 401, message: "Passkey not recognized.")
        }
        return SignInResponse(token: "ocp_passkey", session: Self.session, me: Self.me)
    }
    func session() async throws -> SessionResponse { SessionResponse(session: Self.session, me: Self.me, pushMutedUntil: nil) }
    func signOut() async throws { await server.signOut() }
    func inbox() async throws -> InboxResponse { try await server.list() }
    func approve(id: String, idempotencyKey: String) async throws -> Decision { try await server.approve(id, key: idempotencyKey) }
    func reject(id: String, _ request: RejectRequest, idempotencyKey: String) async throws -> Decision {
        try await server.reject(id, request, key: idempotencyKey)
    }
    func answer(id: String, _ request: TimeAnswerRequest, idempotencyKey: String) async throws -> Decision {
        try await server.answer(id, request.choice, key: idempotencyKey)
    }
    func registerPushToken(_ request: PushTokenRequest) async throws { await server.register(request) }
    func unregisterPushToken() async throws {}

    static let me = ParentMe(adult: .init(id: "a", name: "Alex"), household: HouseholdInfo(id: "h", name: "The Example House"))
    static let session = ParentSessionInfo(id: "s", name: "Test phone", createdAt: Date(), expiresAt: Date().addingTimeInterval(90 * 86_400), lastUsedAt: nil)
}

@MainActor
struct ParentModelTests {
    static func approval(_ id: String, minutesAgo: Double) -> InboxItem {
        InboxItem(id: id, kind: .completion, child: .init(id: "k", name: "Robin"), title: "Chore \(id)", points: 5,
                  submittedAt: Date().addingTimeInterval(-minutesAgo * 60), minutesRequested: nil)
    }

    private func signedIn(_ approvals: [InboxItem]) async -> (ParentModel, FakeParentServer) {
        let server = FakeParentServer(approvals: approvals)
        let unique = UUID().uuidString
        let store = ParentStore(keychain: Keychain(service: "org.opencozy.tests.\(unique)"), defaultsSuite: "org.opencozy.tests.\(unique)")
        let model = ParentModel(store: store, makeAPI: { _, token in FakeParentAPI(server: server, token: token) }, notifier: .none)
        let ok = await model.signIn(server: "https://opencozy.example.org", email: "parent@example.org", password: "correct horse", deviceName: "Test phone")
        #expect(ok)
        return (model, server)
    }

    @Test func signInStoresTokenAndLoadsInboxOldestFirst() async {
        let (model, _) = await signedIn([Self.approval("new", minutesAgo: 1), Self.approval("old", minutesAgo: 30)])
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

    @Test func timeActionAnswersWithAStableKey() async throws {
        let item = InboxItem(id: "t", kind: .timeRequest, child: .init(id: "k", name: "Robin"), title: "More iPad time", points: nil,
                             submittedAt: Date(), minutesRequested: nil)
        let (model, server) = await signedIn([item])
        let push = ApprovalPush(approvalId: "t", event: .pending, kind: "time_request")
        // Approve/Reject don't apply to a time request.
        #expect(await model.handleAction(.approve, push: push) == .failed)
        #expect(await model.handleTimeAction(.oneHour, push: push) == .done)
        let answer = try #require(await server.answers.first)
        #expect(answer.id == "t" && answer.choice == "1h" && answer.key == "s:t:answer:1h")
        #expect(model.approvals.isEmpty)
        // Answered elsewhere: Already handled.
        #expect(await model.handleTimeAction(.no, push: push) == .alreadyHandled)
    }

    @Test func approveSendsAStableKeyAndRemovesItem() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        #expect(await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending)) == .done)
        let (id, key) = try #require(await server.approves.first)
        #expect(id == "a")
        #expect(key == "s:a:approve")
        #expect(model.approvals.isEmpty)
        #expect(model.pendingCount == 0)
    }


    /// A notification action can cold-launch the app before the session loads.
    @Test func coldLaunchActionUsesTheStoredSessionIdOrAsks() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5), Self.approval("b", minutesAgo: 4)])
        #expect(model.store.sessionId == "s")
        let relaunched = ParentModel(store: model.store, makeAPI: { _, t in FakeParentAPI(server: server, token: t) }, notifier: .none)
        #expect(relaunched.session == nil)
        #expect(await relaunched.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending)) == .done)
        model.store.sessionId = nil
        let forgetful = ParentModel(store: model.store, makeAPI: { _, t in FakeParentAPI(server: server, token: t) }, notifier: .none)
        #expect(await forgetful.handleAction(.approve, push: ApprovalPush(approvalId: "b", event: .pending)) == .done)
        #expect(await server.approves.map(\.key) == ["s:a:approve", "s:b:approve"])
        #expect(model.store.sessionId == "s")
    }

    @Test func rejectFromANotificationHasNoReason() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        #expect(await model.handleAction(.reject, push: ApprovalPush(approvalId: "a", event: .pending)) == .done)
        #expect(await server.rejects.map(\.request.reason) == [nil])
        #expect(await server.rejects.map(\.key) == ["s:a:reject"])
    }

    @Test func conflictFromAnotherParentRemovesTheItem() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        // Meanwhile the other parent approved it, so the next list is empty.
        _ = try await server.approve("a", key: "other-session:a:approve")
        #expect(await model.handleAction(.reject, push: ApprovalPush(approvalId: "a", event: .pending)) == .alreadyHandled)
        #expect(model.approvals.isEmpty)
        #expect(model.errorMessage == "Someone already decided this one.")
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

    @Test func notificationActionRetriesWithTheSameKey() async throws {
        let (model, server) = await signedIn([Self.approval("a", minutesAgo: 5)])
        await server.fail([.transport("offline")])
        let ok = await model.handleAction(.approve, push: ApprovalPush(approvalId: "a", event: .pending))
        #expect(ok == .done)
        let approves = await server.approves
        #expect(approves.count == 2)
        #expect(approves.map(\.key) == ["s:a:approve", "s:a:approve"])
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

import CryptoKit
import Foundation
import Observation

/// Parent device settings: the server origin (not secret) and the parent session
/// token (Keychain only).
struct ParentStore: Sendable {
    private static let baseURLKey = "parent.baseURL.v1"
    private static let tokenAccount = "parent-session.v1"
    private static let webHandoffOffKey = "parent.webHandoffOff.v1"
    private static let phoneNameKey = "parent.phoneName.v1"
    private static let sessionIdKey = "parent.sessionId.v1"

    var keychain = Keychain()
    var defaultsSuite: String?

    private var defaults: UserDefaults { defaultsSuite.flatMap(UserDefaults.init(suiteName:)) ?? .standard }

    var baseURL: URL? {
        get { defaults.string(forKey: Self.baseURLKey).flatMap(URL.init(string:)) }
        nonmutating set { defaults.set(newValue?.absoluteString, forKey: Self.baseURLKey) }
    }

    /// The name this phone's notification session gets in Manage → Devices.
    var phoneName: String? {
        get { defaults.string(forKey: Self.phoneNameKey) }
        nonmutating set { defaults.set(newValue, forKey: Self.phoneNameKey) }
    }

    /// Set by Stop notifications, and when an adult revokes this phone (401 on its
    /// session), so a web sign-in doesn't quietly turn notifications back on. Only
    /// a sign-in from the bell sheet clears it.
    var webHandoffOff: Bool {
        get { defaults.bool(forKey: Self.webHandoffOffKey) }
        nonmutating set { defaults.set(newValue, forKey: Self.webHandoffOffKey) }
    }

    /// This phone session's id (not a secret): part of every decision's
    /// idempotency key, so it must be known even on a cold launch from a
    /// notification action, before the session is loaded.
    var sessionId: String? {
        get { defaults.string(forKey: Self.sessionIdKey) }
        nonmutating set { defaults.set(newValue, forKey: Self.sessionIdKey) }
    }

    var token: String? {
        get { keychain.string(for: Self.tokenAccount) }
        nonmutating set { keychain.set(newValue, for: Self.tokenAccount) }
    }
}

/// Parent mode, native side: the server address the web view opens, and this
/// phone's parent session for push and lock-screen Approve/Reject (the approval
/// list is kept only to sync the badge and delivered notifications). The inbox
/// itself is the web app. Never touches Family Controls.
@MainActor
@Observable
final class ParentModel {
    /// The OpenCozy server the web view opens.
    private(set) var origin: URL?
    private(set) var household: HouseholdInfo?
    private(set) var session: ParentSessionInfo? {
        didSet { store.sessionId = session?.id }
    }
    private(set) var approvals: [InboxItem] = []
    private(set) var pendingCount = 0
    private(set) var isLoading = false
    /// Approval IDs with a decision in flight.
    private(set) var busy: Set<String> = []
    /// The last request couldn't reach the server (LAN only: away from home, no VPN).
    private(set) var isUnreachable = false
    var errorMessage: String?

    static let unreachableMessage = WebShell.unreachableMessage
    static let parentModeLater = "Parent mode on the iPhone comes in a later update. Use OpenCozy in the browser for now."
    static let addressHint = "Enter the OpenCozy address, for example https://opencozy.example.org."

    let store: ParentStore
    private let makeAPI: @Sendable (URL, String?) -> any ParentAPI
    /// Badge and delivered-notification housekeeping; replaced in tests.
    private let notifier: ApprovalNotifier
    private let webData: WebDataGate
    private var pushToken: String?
    private var registeredPushToken: String?

    init(store: ParentStore = ParentStore(),
         makeAPI: @escaping @Sendable (URL, String?) -> any ParentAPI = { HTTPParentAPI(baseURL: $0, token: $1) },
         notifier: ApprovalNotifier = .system,
         webData: WebDataGate = .shared) {
        self.store = store
        self.webData = webData
        self.makeAPI = makeAPI
        self.notifier = notifier
        self.origin = store.baseURL
    }

    /// Sets the server the web view opens. Returns false for an address that
    /// isn't https (or this machine).
    @discardableResult
    func connect(_ server: String) -> Bool {
        guard let url = BaseURL.parse(server) else { return false }
        if url != store.baseURL { clearSession() }
        store.baseURL = url
        origin = url
        errorMessage = nil
        return true
    }

    /// Forgets the server (and this phone's notification session), and wipes the
    /// web view's cookies so nothing signed in survives into another role.
    func disconnect() async {
        webData.reset()
        await signOut()
        store.baseURL = nil
        origin = nil
    }

    /// What this phone is called in Manage → Devices: the parent's choice, or a
    /// suggestion that tells phones apart (`DeviceName`).
    var phoneName: String {
        get { store.phoneName ?? DeviceName.suggested() }
        set {
            let trimmed = newValue.trimmingCharacters(in: .whitespacesAndNewlines)
            store.phoneName = trimmed.isEmpty ? nil : String(trimmed.prefix(80))
        }
    }

    var isSignedIn: Bool { store.baseURL != nil && !(store.token ?? "").isEmpty }

    private var api: (any ParentAPI)? {
        guard let url = store.baseURL, let token = store.token, !token.isEmpty else { return nil }
        return makeAPI(url, token)
    }

    // MARK: Sign-in

    /// Returns whether sign-in succeeded. Errors are left in `errorMessage`.
    @discardableResult
    func signIn(server: String, email: String, password: String, deviceName: String) async -> Bool {
        guard let url = BaseURL.parse(server) else {
            errorMessage = Self.addressHint
            return false
        }
        let name = String(deviceName.trimmingCharacters(in: .whitespacesAndNewlines).prefix(80))
        do {
            let result = try await makeAPI(url, nil).signIn(SignInRequest(
                email: email.trimmingCharacters(in: .whitespacesAndNewlines), password: password,
                deviceName: name.isEmpty ? "Parent device" : name))
            store.baseURL = url
            origin = url
            store.token = result.token
            return await signedIn(result.session, result.me.household)
        } catch {
            handle(error, signingIn: true)
            return false
        }
    }

    /// Passkey sign-in. `authenticate` shows the system passkey sheet for the
    /// server's relying party and challenge (injected for tests).
    @discardableResult
    func signInWithPasskey(server: String, deviceName: String,
                           authenticate: @MainActor (_ rpId: String, _ challenge: Data) async throws -> PasskeyAssertion) async -> Bool {
        guard let url = BaseURL.parse(server) else {
            errorMessage = Self.addressHint
            return false
        }
        let api = makeAPI(url, nil)
        do {
            let options = try await api.passkeyOptions()
            guard let challenge = Data(base64URL: options.options.challenge) else { throw APIError.decoding("challenge") }
            let assertion = try await authenticate(options.options.rpId, challenge)
            let name = String(deviceName.trimmingCharacters(in: .whitespacesAndNewlines).prefix(80))
            let result = try await api.signIn(passkey: PasskeySessionRequest(
                challengeId: options.challengeId, credential: AuthenticationResponseJSON(assertion),
                deviceName: name.isEmpty ? "Parent device" : name))
            store.baseURL = url
            origin = url
            store.token = result.token
            return await signedIn(result.session, result.me.household)
        } catch PasskeyAuthenticator.PasskeyError.cancelled {
            errorMessage = nil
            return false
        } catch {
            handle(error, signingIn: true)
            return false
        }
    }

    // MARK: From the web sign-in

    enum WebHandoff: Equatable {
        /// This phone now has its notification session.
        case enabled
        /// Nothing to do: already on, turned off by the parent, no server, or busy.
        case skipped
        /// The web view isn't signed in as an adult (401/403).
        case notSignedIn
        /// The server has no `POST /api/phone/sessions/web` (404/405): manual sign-in only.
        case unsupported
        /// Unreachable or a server error; the bell sheet still works.
        case failed
    }

    private var handingOff = false

    /// Turns the web view's adult sign-in into this phone's notification session
    /// (`POST /api/phone/sessions/web` with the web cookie, sent natively by `mint`),
    /// so the parent doesn't sign in twice. `mint` returns nil when there's no web
    /// cookie to use. Quiet on failure: the manual sign-in in the bell sheet remains.
    @discardableResult
    func enableFromWebSession(deviceName: String,
                              mint: @MainActor (_ deviceName: String) async throws -> (status: Int, body: Data)?) async -> WebHandoff {
        guard let url = origin, !isSignedIn, !store.webHandoffOff, !handingOff else { return .skipped }
        handingOff = true
        defer { handingOff = false }
        let name = String(deviceName.trimmingCharacters(in: .whitespacesAndNewlines).prefix(80))
        do {
            guard let (status, body) = try await mint(name.isEmpty ? "Parent device" : name) else { return .notSignedIn }
            switch status {
            case 200, 201:
                let result = try APIDates.decoder().decode(SignInResponse.self, from: body)
                // The server may have changed while the request was out.
                guard origin == url, !isSignedIn else { return .skipped }
                store.token = result.token
                _ = await signedIn(result.session, result.me.household)
                return .enabled
            // 403: the web sign-in isn't recent enough (or isn't an adult's); the bell sheet remains.
            case 401, 403: return .notSignedIn
            case 404, 405: return .unsupported
            default: return .failed
            }
        } catch {
            return .failed
        }
    }

    /// Stores the session from any sign-in method and loads the inbox.
    func signedIn(_ session: ParentSessionInfo, _ household: HouseholdInfo) async -> Bool {
        store.webHandoffOff = false
        self.session = session
        self.household = household
        errorMessage = nil
        isUnreachable = false
        await registerPushTokenIfNeeded()
        await refresh()
        return true
    }

    /// Stop notifications in the bell sheet: signs this phone's notification
    /// session out and keeps later web sign-ins from turning it back on.
    func stopNotifications() async {
        store.webHandoffOff = true
        await signOut()
    }

    func signOut() async {
        if let api {
            // Best effort: the server also drops the push token when the session goes.
            try? await api.signOut()
        }
        clearSession()
    }

    private func clearSession() {
        store.token = nil
        session = nil
        household = nil
        approvals = []
        pendingCount = 0
        registeredPushToken = nil
        Task { await notifier.sync([], 0) }
    }

    // MARK: Inbox

    func refresh() async {
        guard let api, !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            apply(try await api.inbox())
            if household == nil, let info = try? await api.session() {
                session = info.session
                household = info.me.household
            }
        } catch {
            handle(error)
        }
    }

    private func apply(_ result: InboxResponse) {
        approvals = result.items.sorted { ($0.submittedAt, $0.id) < ($1.submittedAt, $1.id) }
        pendingCount = approvals.count
        isUnreachable = false
        errorMessage = nil
        let ids = Set(approvals.map(\.id)), count = pendingCount
        Task { await notifier.sync(ids, count) }
    }

    enum Outcome: Equatable {
        /// The server recorded (or had already recorded) this decision.
        case done
        /// 404/409: another parent acted (`already_decided`), the child withdrew it, or
        /// the child was archived.
        case alreadyHandled
        case failed
    }

    /// The idempotency key is stable per phone session, item and decision, so a
    /// retried request, or the same notification action delivered twice, replays the
    /// first answer. Keys are per household and tied to the caller, so the session id
    /// keeps a second parent's tap on the same item from reusing the first parent's
    /// key (422 `idempotency_key_reused`); that parent gets 409 `already_decided`.
    /// A reject with a reason adds a short hash of it (the body `RejectRequest` sends),
    /// so a retry with an edited reason is a new key, not a reused one with a
    /// different body (also 422).
    nonisolated static func idempotencyKey(session: String, _ id: String, _ decision: ApprovalDecision, reason: String? = nil) -> String {
        let base = "\(session.lowercased()):\(id.lowercased()):\(decision == .approve ? "approve" : "reject")"
        guard decision == .reject, let sent = RejectRequest(reason: reason).sentReason else { return base }
        let digest = SHA256.hash(data: Data(sent.utf8)).prefix(6).map { String(format: "%02x", $0) }.joined()
        return "\(base):\(digest)"
    }

    /// A time request's answer: one key per phone session, item and choice.
    nonisolated static func answerKey(session: String, _ id: String, _ choice: TimeChoice) -> String {
        "\(session.lowercased()):\(id.lowercased()):answer:\(choice.wire)"
    }

    /// The session id for the key: in memory, else stored, else asked of the server.
    private func sessionIdForKey(_ api: any ParentAPI) async throws -> String {
        if let id = session?.id ?? store.sessionId { return id }
        let info = try await api.session()
        session = info.session
        household = info.me.household
        return info.session.id
    }

    private func decide(_ id: String, decision: ApprovalDecision, reason: String? = nil, choice: TimeChoice? = nil, retries: Int) async -> Outcome {
        guard let api, !busy.contains(id) else { return .failed }
        busy.insert(id)
        defer { busy.remove(id) }
        let key: String
        do {
            let session = try await sessionIdForKey(api)
            key = choice.map { Self.answerKey(session: session, id, $0) } ?? Self.idempotencyKey(session: session, id, decision, reason: reason)
        } catch {
            handle(error)
            return .failed
        }
        var attempt = 0
        while true {
            do {
                switch (choice, decision) {
                case (let choice?, _):
                    _ = try await api.answer(id: id, TimeAnswerRequest(choice: choice.wire), idempotencyKey: key)
                case (nil, .approve):
                    _ = try await api.approve(id: id, idempotencyKey: key)
                case (nil, .reject):
                    _ = try await api.reject(id: id, RejectRequest(reason: reason), idempotencyKey: key)
                }
                removeLocally(id)
                errorMessage = nil
                isUnreachable = false
                await refresh()
                return .done
            } catch let error as APIError where error.isRetryable && attempt < retries {
                attempt += 1
                try? await Task.sleep(for: .seconds(2))
            } catch {
                handle(error)
                // 404/409: someone else acted or it was withdrawn; show the truth.
                if case APIError.server(let status, _) = error, status == 404 || status == 409 {
                    removeLocally(id)
                    let explanation = errorMessage
                    await refresh()
                    // Keep saying why the item went, unless the refresh hit a new problem.
                    if errorMessage == nil { errorMessage = explanation }
                    return .alreadyHandled
                }
                return .failed
            }
        }
    }

    private func removeLocally(_ id: String) {
        if approvals.contains(where: { $0.id == id }) {
            approvals.removeAll { $0.id == id }
            pendingCount = max(0, pendingCount - 1)
        }
    }

    /// A notification action. The system has already required Face ID or the
    /// passcode. Transport failures are retried a few times with the same request ID.
    /// The notification is removed once the item is settled either way.
    @discardableResult
    func handleAction(_ decision: ApprovalDecision, push: ApprovalPush) async -> Outcome {
        // A stale push is fine: the server's 409 already_decided says so.
        guard push.event == .pending, !push.isTimeRequest else { return .failed }
        guard isSignedIn else {
            await notifier.post(Self.notSentTitle, "Turn on notifications in OpenCozy on this phone first.")
            return .failed
        }
        let outcome = await decide(push.approvalId, decision: decision, retries: 2)
        switch outcome {
        case .done:
            await notifier.remove(push.approvalId)
        case .alreadyHandled:
            await notifier.remove(push.approvalId)
            await notifier.post(Self.alreadyHandledTitle, errorMessage ?? "Someone already dealt with this one.")
        case .failed:
            await notifier.post(Self.notSentTitle, errorMessage ?? "Try again in OpenCozy.")
        }
        return outcome
    }

    /// A TIME_REQUEST action: answers once (`POST /api/inbox/{id}/answer`), retrying a lost
    /// response with the same key, and says what happened.
    @discardableResult
    func handleTimeAction(_ choice: TimeChoice, push: ApprovalPush) async -> Outcome {
        guard push.event == .pending, push.isTimeRequest else { return .failed }
        guard isSignedIn else {
            await notifier.post(Self.timeNotSentTitle, "Turn on notifications in OpenCozy on this phone first.")
            return .failed
        }
        let outcome = await decide(push.approvalId, decision: choice == .no ? .reject : .approve, choice: choice, retries: 2)
        switch outcome {
        case .done:
            await notifier.remove(push.approvalId)
        case .alreadyHandled:
            await notifier.remove(push.approvalId)
            await notifier.post(Self.alreadyHandledTitle, errorMessage ?? "Someone already answered.")
        case .failed:
            await notifier.post(Self.timeNotSentTitle, errorMessage ?? "Try again in OpenCozy.")
        }
        return outcome
    }

    static let timeNotSentTitle = "Answer not sent"
    static let notSentTitle = "Approval not sent"
    static let alreadyHandledTitle = "Already handled"

    /// `approval.resolved`: the item left the inbox elsewhere. Drop its delivered
    /// notification (so a stale Approve/Reject can't be tapped later) and set the
    /// badge from the payload, before any network call. The caller refreshes after.
    func handleResolvedPush(_ push: ApprovalPush) async {
        guard push.event == .resolved else { return }
        await notifier.remove(push.approvalId)
        removeLocally(push.approvalId)
        if let count = push.pendingCount {
            pendingCount = max(0, count)
            await notifier.setBadge(pendingCount)
        }
    }

    // MARK: Push

    func didReceivePushToken(_ token: Data) async {
        pushToken = token.hexString
        await registerPushTokenIfNeeded()
    }

    private func registerPushTokenIfNeeded() async {
        guard let api, let pushToken, pushToken != registeredPushToken else { return }
        do {
            try await api.registerPushToken(PushTokenRequest(token: pushToken, environment: .current))
            registeredPushToken = pushToken
        } catch {
            // Not fatal: the inbox still works by pull to refresh. Retried at next launch.
            if case APIError.server(401, _) = error { handle(error) }
        }
    }

    // MARK: Errors

    private func handle(_ error: Error, signingIn: Bool = false) {
        isUnreachable = false
        switch error {
        case APIError.transport:
            isUnreachable = true
            errorMessage = Self.unreachableMessage
        case APIError.server(410, _):
            // This server answers the previous server's parent paths `update_app`; parent
            // mode on the phone moves to this server in a later update (slices 10 and 11).
            errorMessage = Self.parentModeLater
        case APIError.server(401, let message) where signingIn:
            errorMessage = message
        case APIError.server(401, _):
            // An adult revoked this phone (or its session ended). Make it stick: no
            // automatic re-mint from the web sign-in until a manual sign-in here.
            store.webHandoffOff = true
            clearSession()
            errorMessage = "This phone was signed out of notifications. Sign in here to turn them back on."
        case APIError.server(403, _) where signingIn:
            errorMessage = "That account doesn't manage an OpenCozy household."
        case APIError.server(let status, let message) where status < 500:
            errorMessage = message
        case APIError.server:
            errorMessage = "OpenCozy had a problem. Try again in a moment."
        default:
            errorMessage = error.localizedDescription
        }
    }
}

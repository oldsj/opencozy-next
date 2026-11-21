import FamilyControls
import Foundation
import Observation
import UIKit

@MainActor
@Observable
final class AppModel {
    enum Phase: Equatable {
        case needsAuthorization
        case needsSetup
        case ready
    }

    // Screen state
    private(set) var authorizationStatus: AuthorizationStatus
    /// Bumped on pair/unpair so views depending on `isPaired` update.
    private(set) var pairingRevision = 0
    /// This device as the server last described it (name, child, household).
    private(set) var device: DeviceInfo?
    private(set) var unlockedUntil: Date?
    private(set) var lockOutcome: ScreenTimeLock.Outcome?
    private(set) var isRefreshing = false
    private var refreshQueued = false
    private(set) var lastSync: Date?
    private(set) var syncFailed = false
    /// Set when the server rejected this device (401/403); the device is locked.
    private(set) var accessProblem: String?
    /// Orders screen-time responses so an older in-flight fetch can't overwrite a
    /// newer one (for example the fetch made right after a redemption in the web view).
    private var screenTimeRequested = 0
    private var screenTimeApplied = 0
    /// Re-runs the lock when the current window ends while the app is running, so
    /// relocking never depends on the monitor extension alone.
    @ObservationIgnored private var relockTask: Task<Void, Never>?
    /// The stored lock time is the allowance's cap, with more of the window after it.
    @ObservationIgnored private var lockIsCapped = false
    /// How long before a capped lock time an open app re-syncs.
    static let resyncBeforeCap: TimeInterval = 30
    var errorMessage: String?

    /// The child this device belongs to, as the server last said.
    var childId: String? { device?.device.childId }
    var childName: String? { device?.device.childName }

    let config: AppConfig
    let lock: ScreenTimeLock
    private let preview: (any OpenCozyAPI)?

    init(config: AppConfig = AppConfig(), lock: ScreenTimeLock = ScreenTimeLock()) {
        self.config = config
        self.lock = lock
        self.preview = nil
        self.authorizationStatus = AuthorizationCenter.shared.authorizationStatus
        observeAuthorization()
    }

    /// Follows the system's published status instead of sampling it only at launch
    /// and foreground, which can briefly read `.notDetermined` after launch.
    private func observeAuthorization() {
        Task { [weak self] in
            for await status in AuthorizationCenter.shared.$authorizationStatus.values {
                guard let self else { return }
                let wasApproved = self.authorizationStatus == .approved
                self.authorizationStatus = status
                if status == .approved, !wasApproved { await self.refresh() }
            }
        }
    }

    /// SwiftUI previews: fixture API, no Family Controls calls.
    init(preview api: any OpenCozyAPI, config: AppConfig = AppConfig()) {
        self.config = config
        self.lock = ScreenTimeLock(store: SharedStore(suiteName: "preview"))
        self.preview = api
        self.authorizationStatus = .approved
    }

    var isPreview: Bool { preview != nil }

    var phase: Phase {
        if authorizationStatus != .approved { return .needsAuthorization }
        if isPreview { return .ready }
        if !isPaired || lock.selection?.isEmpty ?? true { return .needsSetup }
        return .ready
    }

    private var api: (any OpenCozyAPI)? {
        if let preview { return preview }
        guard let url = config.baseURL, let signer else { return nil }
        return HTTPOpenCozyAPI(baseURL: url, signer: signer)
    }

    /// Loading the Secure Enclave key reads the Keychain, so it's cached and dropped
    /// on pair and unpair.
    @ObservationIgnored private var cachedSigner: DeviceRequestSigner??
    private var signer: DeviceRequestSigner? {
        if let cachedSigner { return cachedSigner }
        let loaded = config.signer
        cachedSigner = .some(loaded)
        return loaded
    }

    /// Whether this device is paired (origin, device ID and a readable key).
    var isPaired: Bool {
        _ = pairingRevision
        if isPreview { return true }
        return config.baseURL != nil && signer != nil
    }

    // MARK: Authorization

    func refreshAuthorizationStatus() {
        guard !isPreview else { return }
        authorizationStatus = AuthorizationCenter.shared.authorizationStatus
    }

    /// Shows the system prompt; on a child's device a parent approves with their
    /// Apple Account (Family Sharing organiser or guardian).
    func requestAuthorization() async {
        guard !isPreview else { return }
        do {
            try await AuthorizationCenter.shared.requestAuthorization(for: .child)
            errorMessage = nil
        } catch {
            errorMessage = "Screen Time permission wasn't granted: \(error.localizedDescription)"
        }
        refreshAuthorizationStatus()
    }

    // MARK: Sync

    /// Enforces the stored window, then refreshes the device and its screen time
    /// from the server. Called on launch, on returning to the foreground,
    /// periodically while open, when the web view reports a redemption
    /// (`screenTimeChanged`) or finishes loading a page, and when a window ends.
    ///
    /// If a refresh is already running, another pass is queued rather than dropped:
    /// the running pass may have read screen time before a redemption existed.
    func refresh() async {
        if !isPreview {
            lockOutcome = lock.reconcile()
            unlockedUntil = currentStoredWindow()
            scheduleRelock()
        }
        guard !isRefreshing else {
            refreshQueued = true
            return
        }
        isRefreshing = true
        defer { isRefreshing = false }
        repeat {
            refreshQueued = false
            await syncOnce()
        } while refreshQueued
    }

    private func syncOnce() async {
        guard phase == .ready, let api else { return }
        do {
            // The server owns which child this device belongs to.
            let info = try await api.device()
            let previous = childId
            device = info
            guard let childId = info.device.childId else { throw SetupError.noChild }
            if let previous, previous != childId { childChanged() }
            try await uploadShieldEvents(api: api)
            try await sendQueuedTimeRequest(api: api, childId: childId)
            await registerPushTokenIfNeeded()
            try await fetchScreenTime(api: api, childId: childId)
            lastSync = Date()
            syncFailed = false
            accessProblem = nil
            errorMessage = nil
        } catch {
            // Keep the last server-confirmed window (already enforced above): an
            // offline device finishes time it was granted but never gains more.
            syncFailed = true
            handleRejection(error)
            errorMessage = error.localizedDescription
        }
    }

    /// Sends "one more minute" uses recorded on the shield, then forgets the ones
    /// the server now holds. Rejections fail the sync like any signed request.
    private func uploadShieldEvents(api: any OpenCozyAPI) async throws {
        let pending = lock.store.pendingEvents
        guard !pending.isEmpty else { return }
        let now = ClockReading.now()
        let recorded = try await api.screenTimeEvents(pending.map { ScreenTimeEventUpload($0, now: now) })
        lock.store.acknowledge(recorded)
    }

    // MARK: Push (kid pushes: a TV is waiting, TV time almost up, a parent answered)

    private var pushToken: String?
    private var registeredPushToken: String?

    func didReceivePushToken(_ token: Data) async {
        pushToken = token.map { String(format: "%02x", $0) }.joined()
        await registerPushTokenIfNeeded()
    }

    /// Sent once per token while paired; a failure retries at the next sync.
    private func registerPushTokenIfNeeded() async {
        guard let api, let pushToken, pushToken != registeredPushToken else { return }
        do {
            try await api.registerPushToken(PushTokenRequest(token: pushToken, environment: .current))
            registeredPushToken = pushToken
        } catch {
            // Pushes are a convenience; the next sync tries again.
        }
    }

    /// Sends a request for more time made on the shield. Kept for a retry only
    /// when it may not have been recorded; a definite answer (sent, or refused
    /// because a parent just said no) clears it.
    private func sendQueuedTimeRequest(api: any OpenCozyAPI, childId: String) async throws {
        guard let queued = lock.store.queuedTimeRequest else { return }
        do {
            _ = try await api.askForTime(childId: childId, requestId: queued.requestId)
            lock.store.queuedTimeRequest = nil
        } catch let error as APIError where !error.isRetryable {
            lock.store.queuedTimeRequest = nil
            if case .server(let status, _) = error, status == 409 { return }
            throw error
        }
    }

    /// The device was reassigned to another child. Nothing of the previous child's
    /// carries over: the device locks until the new child's screen time is applied.
    private func childChanged() {
        unlockedUntil = nil
        if !isPreview { lockOutcome = lock.update(unlockedUntil: nil) }
        // What the previous child did on the shield isn't the new child's to send.
        lock.store.pendingEvents = []
        lock.store.queuedTimeRequest = nil
        lock.store.shieldContent = nil
    }

    /// Fetches and applies screen time, ignoring a response older than one already
    /// applied.
    private func fetchScreenTime(api: any OpenCozyAPI, childId: String) async throws {
        screenTimeRequested += 1
        let ticket = screenTimeRequested
        let (screen, receivedAt) = try await api.screenTime(childId: childId)
        guard ticket > screenTimeApplied else { return }
        screenTimeApplied = ticket
        applyScreenTime(screen, receivedAt: receivedAt, childId: childId)
    }

    /// What a revoked, unassigned or refused device says (the kid view's words).
    static var reconnectMessage: String {
        let device = UIDevice.current.userInterfaceIdiom == .pad ? "iPad" : "iPhone"
        return "Ask an adult to check this \(device). It needs to be connected to OpenCozy again."
    }

    /// A revoked or unassigned device (401) or lost permission (403) locks at once
    /// instead of letting the stored window run on.
    private func handleRejection(_ error: Error) {
        if error as? SetupError == .noChild {
            accessProblem = Self.reconnectMessage
            unlockedUntil = nil
            if !isPreview {
                lockOutcome = lock.lockNow()
                WebDataGate.shared.reset()
            }
            return
        }
        guard case APIError.server(let status, _) = error, status == 401 || status == 403 else { return }
        accessProblem = Self.reconnectMessage
        unlockedUntil = nil
        if !isPreview {
            lockOutcome = lock.lockNow()
            WebDataGate.shared.reset()
        }
    }

    private func applyScreenTime(_ screen: ScreenTime, receivedAt: Date, childId: String) {
        // Another child's time (the device was reassigned mid-sync) never unlocks this one.
        let mine = screen.childId == childId
        let local = mine ? screen.lockAt.map {
            ClockSkew.localDate(for: $0, serverTime: screen.serverTime, receivedAt: receivedAt)
        } : nil
        lockIsCapped = mine && screen.isCapped
        if isPreview {
            unlockedUntil = local
            return
        }
        // The lists first: the reconcile in update applies them.
        if let webFilter = screen.webFilter { lock.store.webFilterLists = webFilter }
        lockOutcome = lock.update(unlockedUntil: local)
        unlockedUntil = currentStoredWindow()
        scheduleRelock()
        storeShieldContent(screen, receivedAt: receivedAt)
    }

    /// What the shield says while locked: the household's message, when
    /// allowed hours open and the banked time, as of this sync.
    private func storeShieldContent(_ screen: ScreenTime, receivedAt: Date) {
        let local = { (date: Date?) in date.map { ClockSkew.localDate(for: $0, serverTime: screen.serverTime, receivedAt: receivedAt) } }
        let hours = screen.hoursNow
        lock.store.shieldContent = ShieldContent(
            message: device?.household.lockMessage ?? ShieldText.defaultMessage,
            bankedSeconds: screen.bankedSeconds,
            opensAt: hours?.open == false ? local(hours?.opensAt) : nil,
            closesAt: hours?.open == true ? local(hours?.closesAt) : nil,
            ask: screen.timeRequest.flatMap { $0.state == .pending ? .pending : $0.state == .declined ? .declined : nil },
            runsUntil: screen.isCapped ? local(screen.unlockedUntil) : nil)
    }

    /// The window only counts as open if the shield is actually lifted.
    private func currentStoredWindow() -> Date? {
        if case .unlocked(let until) = lockOutcome { return until }
        return nil
    }

    /// While the app runs, reconciles (and re-syncs) a second after the window
    /// ends: the shield comes back on time even if no monitor callback arrives. When the
    /// lock time is only the allowance's cap, it re-syncs shortly before instead, so an open
    /// app carries on without the shield flickering on.
    private func scheduleRelock() {
        relockTask?.cancel()
        relockTask = nil
        guard let until = unlockedUntil else { return }
        let delay = lockIsCapped ? until.timeIntervalSinceNow - Self.resyncBeforeCap : until.timeIntervalSinceNow + 1
        relockTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(max(1, delay)))
            guard !Task.isCancelled else { return }
            await self?.refresh()
        }
    }

    // MARK: Web session

    /// A one-time URL that signs the web view in as this device's child
    /// (`POST /api/device/web-session`, signed). Only an exchange URL on the
    /// paired origin is accepted. A revoked or unassigned device locks, as in sync.
    func webSessionURL() async throws -> URL {
        guard let api, let origin = config.baseURL else {
            throw APIError.notConfigured
        }
        do {
            let response = try await api.webSession()
            guard let url = DeviceWebSession.resolve(response.url, origin: origin) else {
                throw APIError.decoding("The sign-in link isn't on \(origin.host() ?? "the OpenCozy server").")
            }
            return url
        } catch {
            handleRejection(error)
            throw error
        }
    }

    // MARK: Parent setup

    enum SetupError: LocalizedError, Equatable {
        case invalidPairingCode
        case noChild

        var errorDescription: String? {
            switch self {
            case .invalidPairingCode: "That isn't an OpenCozy pairing code. In OpenCozy, open Manage → Devices → Pair a device."
            case .noChild: "Ask an adult to check this device. It needs to be connected to OpenCozy again."
            }
        }
    }

    /// Pairs this device: a new Secure Enclave key, registered with the server using
    /// the scanned token or typed code. The previous pairing (if any) is replaced
    /// only once the server accepts the new key.
    func pair(_ input: PairingInput, deviceName: String, pairing: any DevicePairingAPI = HTTPDevicePairingAPI()) async throws -> DeviceInfo {
        let (key, blob) = try config.keys.generate()
        let name = String(deviceName.trimmingCharacters(in: .whitespacesAndNewlines).prefix(80))
        var request = DevicePairRequest(publicKey: key.publicKeyX963.base64EncodedString(),
                                        platform: DevicePlatform.current,
                                        name: name.isEmpty ? "Child's device" : name)
        switch input {
        case .token(_, let token): request.token = token
        case .code(_, let code): request.code = code
        }
        try request.prove(with: key)
        let (info, receivedAt) = try await pairing.pair(origin: input.origin, request)
        config.keys.save(blob)
        config.purgeLegacyCredential()
        config.deviceId = info.device.id
        cachedSigner = nil
        pairingRevision += 1
        config.baseURL = input.origin
        config.clock.observe(serverTime: info.serverTime, receivedAt: receivedAt)
        accessProblem = nil
        device = nil
        await refresh()
        if !isPreview { AppDelegate.registerForPush() }
        return info
    }

    /// Forgets the pairing here and locks. An adult should also revoke the device in
    /// Manage → Devices.
    func unpair() {
        config.forgetPairing()
        cachedSigner = nil
        pairingRevision += 1
        device = nil
        lockOutcome = lock.lockNow()
        unlockedUntil = nil
        if !isPreview { WebDataGate.shared.reset() }
    }

    /// Set when the App Group can't be reached (a signing/capability problem).
    var sharedStorageProblem: String? {
        lock.isSharedStorageAvailable || isPreview ? nil : ScreenTimeLock.SelectionError.sharedStorageUnavailable.localizedDescription
    }

    /// Saves the parent's selection. Returns an error message if it was refused.
    func saveSelection(_ selection: FamilyActivitySelection) async -> String? {
        var problem: String?
        do {
            try lock.saveSelection(selection)
        } catch {
            problem = error.localizedDescription
        }
        await refresh()
        return problem
    }

    /// Saves the apps that are never locked (Messages, FaceTime, Phone, Contacts).
    func saveAlwaysAllowed(_ selection: FamilyActivitySelection) async -> String? {
        var problem: String?
        do { try lock.saveAlwaysAllowed(selection) } catch { problem = error.localizedDescription }
        await refresh()
        return problem
    }

    /// Checks that the monitor extension runs and shares the App Group.
    func runSelfTest() async -> String {
        guard !isPreview else { return "Not available in previews." }
        switch await lock.runSelfTest() {
        case .passed: return "Passed: the monitor extension responded through the shared App Group."
        case .sharedStorageUnavailable: return ScreenTimeLock.SelectionError.sharedStorageUnavailable.localizedDescription
        case .monitoringFailed(let reason): return "Failed: Screen Time refused the test schedule (\(reason))."
        case .noResponse: return "Failed: the monitor extension didn't answer. Check that the app and the monitor share the same App Group, then try again."
        }
    }
}

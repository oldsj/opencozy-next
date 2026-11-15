import DeviceActivity
import FamilyControls
import Foundation
import ManagedSettings

extension ManagedSettingsStore.Name {
    /// One named store, shared by the app and the monitor extension.
    static var openCozy: Self { Self("org.opencozy.screen-time") }
    /// The web filter (blocked websites in every browser), kept apart from the
    /// shield so neither ever clears the other.
    static var webFilter: Self { Self("org.opencozy.web-filter") }
}

extension DeviceActivityName {
    /// The activity that ends the current unlock window.
    static var grant: Self { Self("org.opencozy.grant") }
    /// A second, independent schedule ending a minute after the window, so one
    /// lost callback can't leave the shield off (see `ScreenTimeLock.ensureMonitoring`).
    static var backstop: Self { Self("org.opencozy.backstop") }
    /// Setup self-test: proves the monitor extension runs and shares the App Group.
    static var selfTest: Self { Self("org.opencozy.self-test") }
}

/// What a reconcile should do, decided from stored state and clocks alone.
public enum LockDecision: Equatable, Sendable {
    /// Apply the shield.
    case shield
    /// Keep (or arm, if missing) the schedule for the window, then lift the shield.
    case keepArmed(lockAt: Date)
    /// Arm a new schedule for the remaining window, then lift the shield.
    case rearm(lockAt: Date)
    /// Time remains: change nothing (an interval starting is no reason to arm).
    case unchanged

    /// Who is reconciling.
    public enum Context: Equatable, Sendable {
        /// The app, or the shield-action extension.
        case ordinary
        /// `intervalDidStart` in the extension. It shields an expired window but never
        /// arms: arming from here raced the app, which records the schedule as it arms it.
        case intervalStart
        /// `intervalWillEndWarning` / `intervalDidEnd` in the extension.
        case intervalEnd
    }

    /// An end callback with this little left shields outright, so minute rounding
    /// inside the system can't make the extension re-arm in a loop.
    public static let earlyLockMargin: TimeInterval = 120
    /// An end callback this soon after arming comes from the schedule that arming
    /// just replaced, not from the new one.
    public static let supersededCallbackWindow: TimeInterval = 10

    /// The window left: the server's, or "one more minute" (`bonus`), whichever ends later.
    public static func remaining(state: UnlockState?, bonus: UnlockState?, clock: ClockReading) -> TimeInterval? {
        let times = [UnlockPolicy.remaining(state, clock: clock), UnlockPolicy.remaining(bonus, clock: clock)].compactMap { $0 }
        return times.max()
    }

    public static func decide(state: UnlockState?, bonus: UnlockState? = nil, clock: ClockReading, context: Context,
                              armedAtMonotonic: TimeInterval?) -> LockDecision {
        guard let remaining = remaining(state: state, bonus: bonus, clock: clock) else { return .shield }
        let lockAt = clock.wall.addingTimeInterval(remaining)
        switch context {
        case .ordinary: return .keepArmed(lockAt: lockAt)
        case .intervalStart: return .unchanged
        case .intervalEnd: break
        }
        // Arming overwrites the previous schedule, whose end may fire at once. That
        // callback says nothing about this window, even a short one ("one more
        // minute"). A genuine end of the new schedule has almost nothing left.
        if let armed = armedAtMonotonic, (0...supersededCallbackWindow).contains(clock.monotonic - armed),
           remaining > 2 * supersededCallbackWindow {
            return .keepArmed(lockAt: lockAt)
        }
        // A genuine end callback: the interval was sized to the window, so lock unless
        // time genuinely remains, in which case re-arm. Never leave the shield off
        // without a live schedule.
        if remaining <= earlyLockMargin { return .shield }
        return .rearm(lockAt: lockAt)
    }
}

/// "One more minute": after a real window ends and the shield comes back, the child
/// may lift it for 60 seconds once, from the shield itself and offline. Using it
/// doesn't make it available again: only the end of another server window does.
public enum OneMoreMinute {
    public static let duration: TimeInterval = 60

    public static func isAvailable(store: SharedStore, clock: ClockReading = .now()) -> Bool {
        store.oneMoreMinuteEpoch != store.lockEpoch && UnlockPolicy.remaining(store.oneMoreMinute, clock: clock) == nil
    }

    /// A server window lifted the shield.
    public static func serverWindowOpened(store: SharedStore) {
        if !store.serverWindowSeen { store.serverWindowSeen = true }
    }

    /// The shield is (re)applied. After a server window that is a fresh lock.
    public static func shielded(store: SharedStore) {
        guard store.serverWindowSeen else { return }
        store.lockEpoch += 1
        store.serverWindowSeen = false
    }

    /// Marks it used for this lock and starts the minute.
    public static func start(store: SharedStore, clock: ClockReading) {
        store.oneMoreMinuteEpoch = store.lockEpoch
        store.oneMoreMinute = UnlockState(unlockedUntil: clock.wall.addingTimeInterval(duration), synced: clock)
        store.enqueue(ScreenTimeEvent(id: UUID().uuidString.lowercased(), kind: .oneMoreMinute, at: clock))
    }
}

/// Applies or lifts the shield and keeps the DeviceActivity schedule in step with
/// the stored unlock window. Used by the app and the DeviceActivityMonitor extension.
public struct ScreenTimeLock {
    public enum Outcome: Equatable, Sendable {
        case shielded
        case unlocked(until: Date)
        case noSelection
    }

    public let store: SharedStore
    private let settings = ManagedSettingsStore(named: .openCozy)
    private let webFilter = ManagedSettingsStore(named: .webFilter)

    public init(store: SharedStore = SharedStore()) {
        self.store = store
    }

    /// Whether the App Group container is reachable. If not, nothing can be shared
    /// with the monitor extension and setup must not continue.
    public var isSharedStorageAvailable: Bool { store.isAvailable }

    public var selection: FamilyActivitySelection? {
        guard let data = store.selectionData else { return nil }
        return try? JSONDecoder().decode(FamilyActivitySelection.self, from: data)
    }

    /// Never shielded: the parent's always-allowed apps plus communication apps the
    /// shield extension recognised (`CommunicationApps`).
    public var alwaysAllowed: Set<ApplicationToken> {
        func tokens(_ data: Data?) -> Set<ApplicationToken> {
            data.flatMap { try? JSONDecoder().decode(Set<ApplicationToken>.self, from: $0) } ?? []
        }
        return tokens(store.alwaysAllowedData).union(tokens(store.detectedCommunicationData))
    }

    /// The parent's always-allowed list, for the picker.
    public var alwaysAllowedSelection: FamilyActivitySelection {
        var selection = FamilyActivitySelection()
        selection.applicationTokens = store.alwaysAllowedData.flatMap { try? JSONDecoder().decode(Set<ApplicationToken>.self, from: $0) } ?? []
        return selection
    }

    /// Saves the always-allowed apps (only apps count; categories and websites in
    /// that picker are ignored) and re-applies the shield without them.
    public func saveAlwaysAllowed(_ selection: FamilyActivitySelection) throws {
        guard store.isAvailable else { throw SelectionError.sharedStorageUnavailable }
        store.alwaysAllowedData = try JSONEncoder().encode(selection.applicationTokens)
        if settings.shield.applications != nil || settings.shield.applicationCategories != nil, let current = self.selection {
            shield(current)
        }
    }

    /// Called by the shield-configuration extension when the system asks it to
    /// cover a communication app: remember the token and lift that app's shield now.
    public func exemptCommunicationApp(_ token: ApplicationToken) {
        var detected = store.detectedCommunicationData.flatMap { try? JSONDecoder().decode(Set<ApplicationToken>.self, from: $0) } ?? []
        if detected.insert(token).inserted { store.detectedCommunicationData = try? JSONEncoder().encode(detected) }
        if var apps = settings.shield.applications, apps.remove(token) != nil {
            settings.shield.applications = apps.isEmpty ? nil : apps
        }
        if case .specific(let categories, let except) = settings.shield.applicationCategories, !except.contains(token) {
            settings.shield.applicationCategories = .specific(categories, except: except.union([token]))
        }
    }

    public enum SelectionError: LocalizedError, Equatable {
        case sharedStorageUnavailable
        case empty

        public var errorDescription: String? {
            switch self {
            case .sharedStorageUnavailable:
                "The App Group isn't available, so the lock can't be shared with the monitor extension. Check the App Groups capability on the app and the monitor."
            case .empty:
                "Choose at least one app or category. Websites aren't locked. To stop locking, remove OpenCozy's Screen Time access instead."
            }
        }
    }

    /// Saves a non-empty selection. An empty one is refused: it would leave nothing to
    /// shield, and it must not be a way past the lock.
    public func saveSelection(_ selection: FamilyActivitySelection) throws {
        guard store.isAvailable else { throw SelectionError.sharedStorageUnavailable }
        guard !selection.isEmpty else { throw SelectionError.empty }
        store.selectionData = try JSONEncoder().encode(selection)
    }

    /// Re-derives the shield from the stored window. Called on app launch,
    /// foreground, after each server sync, from every monitor callback and from the
    /// shield's "One more minute" button.
    ///
    /// The web filter follows the outcome: games are open only while unlocked.
    /// Without a selection it fails closed (portals and games blocked).
    @discardableResult
    public func reconcile(clock: ClockReading = .now(), context: LockDecision.Context = .ordinary) -> Outcome {
        let outcome = reconcileShield(clock: clock, context: context)
        let open = if case .unlocked = outcome { true } else { false }
        applyWebFilter(windowOpen: open)
        return outcome
    }

    /// Blocks the household's websites in every browser (WebKit, so Safari and
    /// Chrome alike): game portals always, games unless the window is open.
    private func applyWebFilter(windowOpen: Bool) {
        let plan = WebFilterPlan.plan(store.webFilterLists, windowOpen: windowOpen)
        let policy: WebContentSettings.FilterPolicy? = plan.domains.isEmpty ? nil : .specific(Set(plan.domains.map { WebDomain(domain: $0) }))
        if webFilter.webContent.blockedByFilter != policy { webFilter.webContent.blockedByFilter = policy }
    }

    private func reconcileShield(clock: ClockReading, context: LockDecision.Context) -> Outcome {
        // A missing or unreadable selection never clears anything: existing shields
        // stay as they are. (That covers a misconfigured App Group in the extension,
        // or a selection format change after an OS update.)
        guard let selection, !selection.isEmpty else { return .noSelection }

        let state = store.unlockState
        let decision = LockDecision.decide(state: state, bonus: store.oneMoreMinute, clock: clock, context: context,
                                           armedAtMonotonic: store.armedAtMonotonic)
        switch decision {
        case .shield:
            shieldAfterWindow(selection)
            return .shielded
        case .unchanged:
            return .unlocked(until: clock.wall.addingTimeInterval(LockDecision.remaining(state: state, bonus: store.oneMoreMinute, clock: clock) ?? 0))
        case .keepArmed(let lockAt), .rearm(let lockAt):
            // Only lift the shield while a schedule exists that will put it back. If the
            // system refuses one, forget the window so every process agrees it's locked
            // (fail closed); the next server sync tries again.
            let force = if case .rearm = decision { true } else { false }
            guard ensureMonitoring(lockAt: lockAt, clock: clock, force: force) else {
                if let state { store.unlockState = UnlockState(unlockedUntil: nil, synced: state.synced) }
                store.oneMoreMinute = nil
                shieldAfterWindow(selection)
                return .shielded
            }
            if UnlockPolicy.remaining(state, clock: clock) != nil { OneMoreMinute.serverWindowOpened(store: store) }
            unshield()
            return .unlocked(until: lockAt)
        }
    }

    /// Shields, and counts a fresh lock when a server window just ended: that is
    /// what makes "One more minute" available again.
    private func shieldAfterWindow(_ selection: FamilyActivitySelection) {
        OneMoreMinute.shielded(store: store)
        shield(selection)
    }

    /// The shield's "One more minute" button: lifts the shield for 60 seconds if
    /// it's available, and queues the event for the server's history. Works offline.
    @discardableResult
    public func useOneMoreMinute(clock: ClockReading = .now()) -> Outcome? {
        guard OneMoreMinute.isAvailable(store: store, clock: clock) else { return nil }
        OneMoreMinute.start(store: store, clock: clock)
        return reconcile(clock: clock)
    }

    /// Records a fresh server window (already in local time) and reconciles.
    @discardableResult
    public func update(unlockedUntil: Date?, clock: ClockReading = .now()) -> Outcome {
        store.unlockState = UnlockState(unlockedUntil: unlockedUntil, synced: clock)
        return reconcile(clock: clock)
    }

    /// Forgets the window and shields immediately: credential removed, revoked (401)
    /// or no longer permitted (403).
    @discardableResult
    public func lockNow() -> Outcome {
        store.unlockState = nil
        store.oneMoreMinute = nil
        let center = DeviceActivityCenter()
        let ours = center.activities.filter { $0 == .grant || $0 == .backstop }
        if !ours.isEmpty { center.stopMonitoring(ours) }
        store.scheduledLockAt = nil
        store.scheduledEnd = nil
        return reconcile()
    }

    private func shield(_ selection: FamilyActivitySelection) {
        // Communication and other always-allowed apps are taken out of the app
        // list and exempted from the category shield.
        let plan = ShieldPlan.make(applications: selection.applicationTokens, categories: selection.categoryTokens, alwaysAllowed: alwaysAllowed)
        settings.shield.applications = plan.applications
        settings.shield.applicationCategories = plan.categories.map { .specific($0, except: plan.categoryExceptions) }
        // Websites are never shielded: browsers stay open (Wikipedia, school sites).
        // Picked categories would otherwise block every site in them, in every
        // browser. Cleared explicitly so an earlier version's web shields go away.
        settings.shield.webDomains = nil
        settings.shield.webDomainCategories = nil
    }

    private func unshield() {
        settings.shield.applications = nil
        settings.shield.applicationCategories = nil
        settings.shield.webDomains = nil
        settings.shield.webDomainCategories = nil
    }

    /// Arms the grant schedule for `lockAt` and its backstop, unless the same ones
    /// are already armed.
    ///
    /// No `stopMonitoring` first: `startMonitoring` overwrites an existing schedule,
    /// whereas stopping an ongoing interval fires `intervalDidEnd` in the extension.
    /// Skipping unchanged schedules avoids the "too tightly scheduled" error Apple
    /// warns about when an app restarts monitoring repeatedly. "Unchanged" is checked
    /// against the system's own record (`schedule(for:)`), not just what was stored.
    private func ensureMonitoring(lockAt: Date, clock: ClockReading, force: Bool) -> Bool {
        let center = DeviceActivityCenter()
        if !force, let armed = store.scheduledLockAt, abs(armed.timeIntervalSince(lockAt)) < 2,
           let end = store.scheduledEnd, end > clock.wall, Self.isArmed(center, .grant, end: end),
           center.activities.contains(.backstop) {
            return true
        }
        guard let plan = MonitorPlan.make(now: clock.wall, lockAt: lockAt) else { return false }
        let backstop = MonitorPlan.backstop(now: clock.wall, lockAt: lockAt)
        // Recorded before arming: overwriting a schedule may fire the old one's end
        // callback straight away, in the extension, while this call is still running,
        // and the extension must recognise both the callback and the new schedule.
        let previous = (store.scheduledLockAt, store.scheduledEnd, store.armedAtMonotonic)
        store.armedAtMonotonic = clock.monotonic
        store.scheduledLockAt = lockAt
        store.scheduledEnd = plan.end
        do {
            try center.startMonitoring(.grant, during: DeviceActivitySchedule(
                intervalStart: plan.startComponents(), intervalEnd: plan.endComponents(), repeats: false, warningTime: plan.warningComponents))
        } catch {
            (store.scheduledLockAt, store.scheduledEnd, store.armedAtMonotonic) = (nil, nil, previous.2)
            return false
        }
        // The backstop is best effort: the grant schedule alone is enough to lift.
        try? center.startMonitoring(.backstop, during: DeviceActivitySchedule(
            intervalStart: backstop.startComponents(), intervalEnd: backstop.endComponents(), repeats: false))
        return true
    }

    /// Whether the system still holds a schedule for `activity` ending at `end`.
    static func isArmed(_ center: DeviceActivityCenter, _ activity: DeviceActivityName, end: Date) -> Bool {
        guard center.activities.contains(activity), let schedule = center.schedule(for: activity),
              let scheduledEnd = Calendar.current.date(from: schedule.intervalEnd) else { return false }
        return abs(scheduledEnd.timeIntervalSince(end)) < 2
    }

    // MARK: Setup self-test

    public enum SelfTestResult: Equatable, Sendable {
        case passed
        case sharedStorageUnavailable
        case monitoringFailed(String)
        case noResponse
    }

    /// Arms a throwaway schedule whose `intervalDidStart` makes the monitor extension
    /// echo a nonce through the App Group. Success proves the extension runs and that
    /// both processes read the same store.
    @MainActor
    public func runSelfTest(timeout: Duration = .seconds(10)) async -> SelfTestResult {
        guard store.isAvailable else { return .sharedStorageUnavailable }
        let nonce = UUID().uuidString
        store.selfTestEcho = nil
        store.selfTestNonce = nonce
        let center = DeviceActivityCenter()
        let now = Date()
        let plan = MonitorPlan(start: now, end: now.addingTimeInterval(MonitorPlan.minimumInterval), warningMinutes: nil)
        do {
            try center.startMonitoring(.selfTest, during: DeviceActivitySchedule(
                intervalStart: plan.startComponents(), intervalEnd: plan.endComponents(), repeats: false))
        } catch {
            return .monitoringFailed(error.localizedDescription)
        }
        defer { center.stopMonitoring([.selfTest]) }
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: timeout)
        while clock.now < deadline {
            if store.selfTestEcho == nonce { return .passed }
            try? await Task.sleep(for: .milliseconds(250))
        }
        return store.selfTestEcho == nonce ? .passed : .noResponse
    }

    /// Called by the monitor extension when the self-test interval starts.
    public func answerSelfTest() {
        store.selfTestEcho = store.selfTestNonce
    }
}

extension FamilyActivitySelection {
    var isEmpty: Bool {
        // Websites don't count: they are never shielded, so they would lock nothing.
        applicationTokens.isEmpty && categoryTokens.isEmpty
    }
}

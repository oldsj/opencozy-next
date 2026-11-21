import Foundation

/// State shared between the app and its extensions through the App Group.
///
/// Only non-secret values live here: the parent's app selection (opaque tokens)
/// and the current unlock window. The API credential stays in the app's Keychain.
public enum AppGroup {
    /// Read from each target's Info.plist (`OpenCozyAppGroup`), which XcodeGen fills
    /// from the `OPENCOZY_APP_GROUP` build setting, so no identifier is hard-coded.
    public static var identifier: String {
        Bundle.main.object(forInfoDictionaryKey: "OpenCozyAppGroup") as? String ?? ""
    }

    /// The App Group identifier, only if this process is entitled to its container.
    /// There is deliberately no fallback to standard defaults: a misconfigured group
    /// must surface as an error, not silently split app and extension state.
    public static var verifiedIdentifier: String? {
        let id = identifier
        guard !id.isEmpty, FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: id) != nil else { return nil }
        return id
    }
}

/// A reading of the device's clocks, taken together.
///
/// The wall clock can be changed (by a person or the network); `monotonic` can't. It's
/// `CLOCK_MONOTONIC`, which keeps counting while the device sleeps and restarts at
/// boot. Comparing the two since the last sync reveals a clock change, and the boot
/// session ID (or the monotonic clock going backwards) reveals a reboot.
public struct ClockReading: Codable, Equatable, Sendable {
    public var wall: Date
    public var monotonic: TimeInterval
    public var bootSession: String?
    public var timeZone: String

    public init(wall: Date, monotonic: TimeInterval, bootSession: String?, timeZone: String) {
        self.wall = wall
        self.monotonic = monotonic
        self.bootSession = bootSession
        self.timeZone = timeZone
    }

    public static func now() -> ClockReading {
        ClockReading(
            wall: Date(),
            monotonic: TimeInterval(clock_gettime_nsec_np(CLOCK_MONOTONIC)) / 1_000_000_000,
            bootSession: bootSessionID(),
            timeZone: TimeZone.current.identifier
        )
    }

    private static func bootSessionID() -> String? {
        var size = 0
        guard sysctlbyname("kern.bootsessionuuid", nil, &size, nil, 0) == 0, size > 0 else { return nil }
        var buffer = [CChar](repeating: 0, count: size)
        guard sysctlbyname("kern.bootsessionuuid", &buffer, &size, nil, 0) == 0 else { return nil }
        return String(decoding: buffer.prefix { $0 != 0 }.map(UInt8.init(bitPattern:)), as: UTF8.self)
    }

    public func advanced(by seconds: TimeInterval) -> ClockReading {
        ClockReading(wall: wall.addingTimeInterval(seconds), monotonic: monotonic + seconds, bootSession: bootSession, timeZone: timeZone)
    }
}

/// The unlock window as last confirmed by the server, already converted to this
/// device's clock (see `ClockSkew`), with the clock reading taken at that sync.
public struct UnlockState: Codable, Equatable, Sendable {
    /// Local wall-clock instant at which the selection must be shielded again. `nil` = locked.
    public var unlockedUntil: Date?
    /// Clocks at the sync that produced this window.
    public var synced: ClockReading

    public init(unlockedUntil: Date?, synced: ClockReading) {
        self.unlockedUntil = unlockedUntil
        self.synced = synced
    }

    /// Wall-clock time the window was synced; kept for display.
    public var syncedAt: Date { synced.wall }
}

public enum UnlockPolicy {
    /// Grace period that treats a window ending this soon as already over, so a
    /// warning callback that fires a moment early still re-applies the shield.
    public static let endTolerance: TimeInterval = 5
    /// How far the wall clock may drift from the monotonic clock between syncs before
    /// it counts as a clock change. Real drift is milliseconds per day.
    public static let maxClockDrift: TimeInterval = 60

    /// Seconds of unlock left, or `nil` when locked.
    ///
    /// Fails closed: no state, no window, a reboot since the sync, a wall-clock change,
    /// or a time-zone change all mean locked until the app syncs with the server again.
    /// Otherwise the shorter of the wall-clock and monotonic remainders counts.
    public static func remaining(_ state: UnlockState?, clock: ClockReading) -> TimeInterval? {
        guard let state, let until = state.unlockedUntil else { return nil }
        let synced = state.synced
        if let then = synced.bootSession, let now = clock.bootSession, then != now { return nil }
        let elapsedMonotonic = clock.monotonic - synced.monotonic
        guard elapsedMonotonic >= 0 else { return nil }
        let elapsedWall = clock.wall.timeIntervalSince(synced.wall)
        guard abs(elapsedWall - elapsedMonotonic) <= maxClockDrift else { return nil }
        guard synced.timeZone == clock.timeZone else { return nil }
        let wallRemaining = until.timeIntervalSince(clock.wall)
        let monotonicRemaining = until.timeIntervalSince(synced.wall) - elapsedMonotonic
        let left = min(wallRemaining, monotonicRemaining)
        return left > endTolerance ? left : nil
    }

    public static func isUnlocked(_ state: UnlockState?, clock: ClockReading) -> Bool {
        remaining(state, clock: clock) != nil
    }
}

public enum ClockSkew {
    /// Converts a server timestamp to the local clock using the response's `serverTime`.
    /// `receivedAt` is the local time the response arrived. Network latency can shift
    /// the result by up to one round trip, a few hundred milliseconds in practice.
    public static func localDate(for serverDate: Date, serverTime: Date, receivedAt: Date) -> Date {
        serverDate.addingTimeInterval(receivedAt.timeIntervalSince(serverTime))
    }
}

/// Something the child did on the shield, recorded offline and sent by the app
/// (`POST /api/device/screen-time-events`). `id` makes the upload idempotent.
public struct ScreenTimeEvent: Codable, Equatable, Sendable {
    public enum Kind: String, Codable, Sendable {
        case oneMoreMinute = "one_more_minute"
    }

    public var id: String
    public var kind: Kind
    public var at: ClockReading

    public init(id: String, kind: Kind, at: ClockReading) {
        self.id = id
        self.kind = kind
        self.at = at
    }

    /// Seconds since the event, by the monotonic clock in the same boot session,
    /// otherwise by the wall clock; never negative.
    public func secondsAgo(now: ClockReading) -> Int {
        let sameBoot = at.bootSession != nil && at.bootSession == now.bootSession && now.monotonic >= at.monotonic
        let elapsed = sameBoot ? now.monotonic - at.monotonic : now.wall.timeIntervalSince(at.wall)
        return max(0, Int(elapsed.rounded()))
    }
}

/// "Ask for more time", pressed on the shield. `requestId` is the server
/// command's replay key, so the request is created once however often it's sent.
public struct QueuedTimeRequest: Codable, Equatable, Sendable {
    public var requestId: String
    public var at: Date

    public init(requestId: String, at: Date) {
        self.requestId = requestId
        self.at = at
    }
}

/// Keys and codecs for the App Group store.
public struct SharedStore: Sendable {
    private static let selectionKey = "familyActivitySelection.v1"
    private static let unlockKey = "unlockState.v1"
    private static let scheduledKey = "scheduledLockAt.v1"
    private static let armedKey = "armedAtMonotonic.v1"
    private static let selfTestNonceKey = "selfTestNonce.v1"
    private static let selfTestEchoKey = "selfTestEcho.v1"
    private static let alwaysAllowedKey = "alwaysAllowed.v1"
    private static let detectedCommunicationKey = "detectedCommunication.v1"
    private static let shieldContentKey = "shieldContent.v1"
    private static let scheduledEndKey = "scheduledEnd.v1"
    private static let oneMoreMinuteKey = "oneMoreMinute.v1"
    private static let oneMoreMinuteEpochKey = "oneMoreMinuteEpoch.v1"
    private static let lockEpochKey = "lockEpoch.v1"
    private static let serverWindowSeenKey = "serverWindowSeen.v1"
    private static let eventsKey = "pendingEvents.v1"
    private static let timeRequestKey = "timeRequest.v1"
    private static let webFilterKey = "webFilter.v1"

    private let suiteName: String?

    /// The real App Group store. Unavailable (reads nil, writes ignored) when the
    /// process lacks the App Group entitlement.
    public init() {
        self.suiteName = AppGroup.verifiedIdentifier
    }

    /// An explicit suite, for tests and previews. `nil` models a process without
    /// the App Group: reads return nil and writes are ignored.
    public init(suiteName: String?) {
        self.suiteName = suiteName
    }

    public var isAvailable: Bool { defaults != nil }

    private var defaults: UserDefaults? { suiteName.flatMap(UserDefaults.init(suiteName:)) }

    public var selectionData: Data? {
        get { defaults?.data(forKey: Self.selectionKey) }
        nonmutating set { defaults?.set(newValue, forKey: Self.selectionKey) }
    }

    /// Apps the parent marked always allowed (encoded `Set<ApplicationToken>`).
    public var alwaysAllowedData: Data? {
        get { defaults?.data(forKey: Self.alwaysAllowedKey) }
        nonmutating set { defaults?.set(newValue, forKey: Self.alwaysAllowedKey) }
    }

    /// Communication apps the shield extension recognised by bundle identifier
    /// (encoded `Set<ApplicationToken>`). Kept apart from the parent's list so
    /// editing that list never drops them.
    public var detectedCommunicationData: Data? {
        get { defaults?.data(forKey: Self.detectedCommunicationKey) }
        nonmutating set { defaults?.set(newValue, forKey: Self.detectedCommunicationKey) }
    }

    public var unlockState: UnlockState? {
        get { decode(UnlockState.self, Self.unlockKey) }
        nonmutating set { encode(newValue, Self.unlockKey) }
    }

    /// The window end the current DeviceActivity schedule was armed for.
    public var scheduledLockAt: Date? {
        get { decode(Date.self, Self.scheduledKey) }
        nonmutating set { encode(newValue, Self.scheduledKey) }
    }

    /// Monotonic time at which the grant schedule was last armed.
    public var armedAtMonotonic: TimeInterval? {
        get { decode(TimeInterval.self, Self.armedKey) }
        nonmutating set { encode(newValue, Self.armedKey) }
    }

    /// Setup self-test: the app writes a nonce, the monitor extension echoes it.
    public var selfTestNonce: String? {
        get { defaults?.string(forKey: Self.selfTestNonceKey) }
        nonmutating set { defaults?.set(newValue, forKey: Self.selfTestNonceKey) }
    }

    public var selfTestEcho: String? {
        get { defaults?.string(forKey: Self.selfTestEchoKey) }
        nonmutating set { defaults?.set(newValue, forKey: Self.selfTestEchoKey) }
    }

    /// When the armed grant schedule's interval ends (after `scheduledLockAt` for a
    /// stretched short window).
    public var scheduledEnd: Date? {
        get { decode(Date.self, Self.scheduledEndKey) }
        nonmutating set { encode(newValue, Self.scheduledEndKey) }
    }

    /// "One more minute", while it lasts: a 60-second window from the shield button.
    public var oneMoreMinute: UnlockState? {
        get { decode(UnlockState.self, Self.oneMoreMinuteKey) }
        nonmutating set { encode(newValue, Self.oneMoreMinuteKey) }
    }

    /// Counts locks that followed a server window. "One more minute" is available
    /// while `oneMoreMinuteEpoch` (the lock it was used in) differs.
    public var lockEpoch: Int {
        get { decode(Int.self, Self.lockEpochKey) ?? 0 }
        nonmutating set { encode(newValue, Self.lockEpochKey) }
    }

    public var oneMoreMinuteEpoch: Int? {
        get { decode(Int.self, Self.oneMoreMinuteEpochKey) }
        nonmutating set { encode(newValue, Self.oneMoreMinuteEpochKey) }
    }

    /// A server window has lifted the shield since the last lock.
    public var serverWindowSeen: Bool {
        get { decode(Bool.self, Self.serverWindowSeenKey) ?? false }
        nonmutating set { encode(newValue, Self.serverWindowSeenKey) }
    }

    /// Events recorded offline (by the shield) for the app to send at its next sync.
    public var pendingEvents: [ScreenTimeEvent] {
        get { decode([ScreenTimeEvent].self, Self.eventsKey) ?? [] }
        nonmutating set { encode(newValue.isEmpty ? nil : newValue, Self.eventsKey) }
    }

    /// Keeps at most the 50 newest events, so a device offline for long can't grow the store.
    public func enqueue(_ event: ScreenTimeEvent) {
        pendingEvents = Array((pendingEvents + [event]).suffix(50))
    }

    /// Removes events the server has recorded.
    public func acknowledge(_ ids: Set<String>) {
        pendingEvents = pendingEvents.filter { !ids.contains($0.id) }
    }

    /// An "Ask for more time" from the shield, until the app has sent it.
    public var queuedTimeRequest: QueuedTimeRequest? {
        get { decode(QueuedTimeRequest.self, Self.timeRequestKey) }
        nonmutating set { encode(newValue, Self.timeRequestKey) }
    }

    /// The household's blocked websites, from the last sync (`WebFilterPlan`).
    public var webFilterLists: WebFilterLists? {
        get { decode(WebFilterLists.self, Self.webFilterKey) }
        nonmutating set { encode(newValue, Self.webFilterKey) }
    }

    /// The shield's text, from the last sync (`ShieldText`).
    public var shieldContent: ShieldContent? {
        get { decode(ShieldContent.self, Self.shieldContentKey) }
        nonmutating set { encode(newValue, Self.shieldContentKey) }
    }

    private func decode<T: Decodable>(_ type: T.Type, _ key: String) -> T? {
        guard let data = defaults?.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(type, from: data)
    }

    private func encode<T: Encodable>(_ value: T?, _ key: String) {
        if let value, let data = try? JSONEncoder().encode(value) {
            defaults?.set(data, forKey: key)
        } else {
            defaults?.removeObject(forKey: key)
        }
    }
}

/// "Ask for more time" from the shield: queued here (the extension cannot sign
/// requests) and sent by the app with the same request ID until the server
/// answers, so a lost response never asks twice.
public enum TimeRequestQueue {
    public static func ask(store: SharedStore, now: Date = Date()) {
        if store.queuedTimeRequest == nil { store.queuedTimeRequest = QueuedTimeRequest(requestId: UUID().uuidString.lowercased(), at: now) }
    }
}

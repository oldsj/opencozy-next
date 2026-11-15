import Foundation
import Testing
@testable import OpenCozy

struct MonitorPlanTests {
    let now = Date(timeIntervalSince1970: 1_800_000_000)

    @Test func longWindowEndsExactlyAtLockTime() throws {
        let plan = try #require(MonitorPlan.make(now: now, lockAt: now.addingTimeInterval(30 * 60)))
        #expect(plan.end == now.addingTimeInterval(30 * 60))
        #expect(plan.warningMinutes == nil)
        #expect(plan.lockAt == plan.end)
    }

    @Test(arguments: [10.0, 60.0, 300.0, 870.0, 900.0, 959.0] as [Double])
    func shortWindowUsesWarningAtLockTime(seconds: Double) throws {
        let lockAt = now.addingTimeInterval(seconds)
        let plan = try #require(MonitorPlan.make(now: now, lockAt: lockAt))
        #expect(plan.end.timeIntervalSince(plan.start) >= MonitorPlan.minimumInterval)
        #expect(plan.lockAt == lockAt)
        #expect((plan.warningMinutes ?? 0) > 0)
        #expect(plan.warningComponents == DateComponents(minute: plan.warningMinutes))
    }

    @Test func expiredWindowHasNoPlan() {
        #expect(MonitorPlan.make(now: now, lockAt: now) == nil)
        #expect(MonitorPlan.make(now: now, lockAt: now.addingTimeInterval(-60)) == nil)
        #expect(MonitorPlan.make(now: now, lockAt: now.addingTimeInterval(UnlockPolicy.endTolerance)) == nil)
    }

    @Test func veryLongWindowIsCappedBelowAWeek() throws {
        let plan = try #require(MonitorPlan.make(now: now, lockAt: now.addingTimeInterval(30 * 24 * 3600)))
        #expect(plan.end.timeIntervalSince(plan.start) <= MonitorPlan.maximumInterval)
    }

    @Test func componentsRoundTrip() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let plan = try #require(MonitorPlan.make(now: now, lockAt: now.addingTimeInterval(20 * 60)))
        #expect(calendar.date(from: plan.endComponents(calendar: calendar)) == plan.end)
    }

    @Test func componentsCarryTheTimeZone() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "America/New_York")!
        let plan = try #require(MonitorPlan.make(now: now, lockAt: now.addingTimeInterval(20 * 60)))
        let end = plan.endComponents(calendar: calendar)
        #expect(end.timeZone == calendar.timeZone)
        // Read back in a different zone, the components still name the same instant.
        var elsewhere = Calendar(identifier: .gregorian)
        elsewhere.timeZone = TimeZone(identifier: "Asia/Tokyo")!
        #expect(elsewhere.date(from: end) == plan.end)
    }
}

struct UnlockPolicyTests {
    let synced = ClockReading(wall: Date(timeIntervalSince1970: 1_800_000_000), monotonic: 5_000, bootSession: "boot-a", timeZone: "Europe/London")
    var state: UnlockState { UnlockState(unlockedUntil: synced.wall.addingTimeInterval(30 * 60), synced: synced) }

    @Test func failsClosedWithoutAWindow() {
        #expect(UnlockPolicy.remaining(nil, clock: synced) == nil)
        #expect(UnlockPolicy.remaining(UnlockState(unlockedUntil: nil, synced: synced), clock: synced) == nil)
        #expect(UnlockPolicy.remaining(state, clock: synced.advanced(by: 30 * 60)) == nil)
        #expect(UnlockPolicy.remaining(state, clock: synced.advanced(by: 30 * 60 - 3)) == nil)
    }

    @Test func countsDownNormally() {
        let left = try! #require(UnlockPolicy.remaining(state, clock: synced.advanced(by: 10 * 60)))
        #expect(abs(left - 20 * 60) < 0.001)
    }

    @Test func wallClockSetBackIsLocked() {
        var clock = synced.advanced(by: 10 * 60)
        clock.wall = clock.wall.addingTimeInterval(-3600)
        #expect(UnlockPolicy.remaining(state, clock: clock) == nil)
    }

    @Test func wallClockSetForwardIsLocked() {
        var clock = synced.advanced(by: 60)
        clock.wall = clock.wall.addingTimeInterval(600)
        #expect(UnlockPolicy.remaining(state, clock: clock) == nil)
    }

    @Test func smallDriftIsTolerated() {
        var clock = synced.advanced(by: 600)
        clock.wall = clock.wall.addingTimeInterval(2)
        let left = try! #require(UnlockPolicy.remaining(state, clock: clock))
        #expect(abs(left - (20 * 60 - 2)) < 0.001)
    }

    @Test func rebootWithoutResyncIsLocked() {
        var clock = synced.advanced(by: 60)
        clock.bootSession = "boot-b"
        #expect(UnlockPolicy.remaining(state, clock: clock) == nil)
        // Without a boot session ID, a monotonic clock that went backwards also means a reboot.
        var noID = state
        noID.synced.bootSession = nil
        var restarted = synced.advanced(by: 60)
        restarted.bootSession = nil
        restarted.monotonic = 30
        #expect(UnlockPolicy.remaining(noID, clock: restarted) == nil)
    }

    @Test func timeZoneChangeIsLocked() {
        var clock = synced.advanced(by: 60)
        clock.timeZone = "Asia/Tokyo"
        #expect(UnlockPolicy.remaining(state, clock: clock) == nil)
    }

    @Test func liveClockReading() {
        let a = ClockReading.now()
        let b = ClockReading.now()
        #expect(b.monotonic >= a.monotonic)
        #expect(a.timeZone == TimeZone.current.identifier)
    }

    @Test func clockSkewShiftsServerTimesToLocalClock() {
        let server = Date(timeIntervalSince1970: 1_000)
        // Device clock runs 90 s ahead of the server.
        let local = ClockSkew.localDate(for: server.addingTimeInterval(600), serverTime: server, receivedAt: server.addingTimeInterval(90))
        #expect(local == server.addingTimeInterval(690))
    }

    @Test func sharedStoreRoundTrip() {
        let store = SharedStore(suiteName: "opencozy.tests.\(UUID().uuidString)")
        #expect(store.unlockState == nil)
        store.unlockState = state
        #expect(store.unlockState == state)
        store.unlockState = nil
        #expect(store.unlockState == nil)
    }
}

struct LockDecisionTests {
    let synced = ClockReading(wall: Date(timeIntervalSince1970: 1_800_000_000), monotonic: 5_000, bootSession: "boot-a", timeZone: "UTC")
    var state: UnlockState { UnlockState(unlockedUntil: synced.wall.addingTimeInterval(30 * 60), synced: synced) }

    @Test func noWindowShields() {
        #expect(LockDecision.decide(state: nil, clock: synced, context: .ordinary, armedAtMonotonic: nil) == .shield)
        #expect(LockDecision.decide(state: nil, clock: synced, context: .intervalEnd, armedAtMonotonic: nil) == .shield)
    }

    @Test func ordinaryKeepsSchedule() {
        let clock = synced.advanced(by: 60)
        #expect(LockDecision.decide(state: state, clock: clock, context: .ordinary, armedAtMonotonic: synced.monotonic)
                == .keepArmed(lockAt: state.unlockedUntil!))
    }

    @Test func endCallbackNearTheEndShieldsUnconditionally() {
        // The finding's scenario: the end fires 40 s early because of minute rounding.
        let clock = synced.advanced(by: 30 * 60 - 40)
        #expect(LockDecision.decide(state: state, clock: clock, context: .intervalEnd, armedAtMonotonic: synced.monotonic) == .shield)
    }

    @Test func earlyEndCallbackWithTimeLeftRearms() {
        // e.g. a capped (week-long) schedule ending, or a time-zone quirk.
        let clock = synced.advanced(by: 10 * 60)
        #expect(LockDecision.decide(state: state, clock: clock, context: .intervalEnd, armedAtMonotonic: synced.monotonic)
                == .rearm(lockAt: state.unlockedUntil!))
    }

    @Test func endCallbackRightAfterArmingIsFromTheReplacedSchedule() {
        let clock = synced.advanced(by: 10 * 60)
        #expect(LockDecision.decide(state: state, clock: clock, context: .intervalEnd, armedAtMonotonic: clock.monotonic - 3)
                == .keepArmed(lockAt: state.unlockedUntil!))
    }

    @Test func endCallbackAfterClockChangeShields() {
        var clock = synced.advanced(by: 10 * 60)
        clock.wall = clock.wall.addingTimeInterval(-3600)
        #expect(LockDecision.decide(state: state, clock: clock, context: .intervalEnd, armedAtMonotonic: nil) == .shield)
    }
}

struct SelectionSafetyTests {
    @Test func missingAppGroupNeverReportsASelection() throws {
        let lock = ScreenTimeLock(store: SharedStore(suiteName: nil))
        #expect(!lock.isSharedStorageAvailable)
        #expect(lock.selection == nil)
        // Without a readable selection, reconcile leaves shields alone.
        #expect(lock.reconcile() == .noSelection)
        #expect(throws: ScreenTimeLock.SelectionError.sharedStorageUnavailable) {
            try lock.saveSelection(.init(includeEntireCategory: true))
        }
    }

    @Test func emptySelectionIsRefusedAndClearsNothing() throws {
        let lock = ScreenTimeLock(store: SharedStore(suiteName: "opencozy.tests.\(UUID().uuidString)"))
        #expect(throws: ScreenTimeLock.SelectionError.empty) {
            try lock.saveSelection(.init(includeEntireCategory: true))
        }
        #expect(lock.reconcile() == .noSelection)
    }

    @Test func undecodableSelectionClearsNothing() {
        let store = SharedStore(suiteName: "opencozy.tests.\(UUID().uuidString)")
        store.selectionData = Data("not a selection".utf8)
        let lock = ScreenTimeLock(store: store)
        #expect(lock.selection == nil)
        #expect(lock.reconcile() == .noSelection)
    }

    @Test func appGroupWithoutEntitlementIsRejected() {
        #expect(SharedStore(suiteName: nil).isAvailable == false)
    }
}

struct DecodingTests {
    func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try APIDates.decoder().decode(type, from: Data(json.utf8))
    }

    @Test func screenTimeResponse() throws {
        let json = """
        { "serverTime": "2026-11-07T10:00:00.000Z",
          "children": [ { "childId": "c1", "target": "personal", "unlockedUntil": "2026-11-07T10:45:00.000Z",
            "activeGrants": [
              { "redemptionId": "r1", "minutes": 30, "startsAt": "2026-11-07T09:45:00.000Z", "endsAt": "2026-11-07T10:15:00.000Z" },
              { "redemptionId": "r2", "minutes": 30, "startsAt": "2026-11-07T10:15:00.000Z", "endsAt": "2026-11-07T10:45:00.000Z" } ] } ] }
        """
        let r = try decode(ScreenTimeResponse.self, json)
        #expect(r.children.count == 1)
        #expect(r.children[0].activeGrants.count == 2)
        #expect(r.children[0].unlockedUntil!.timeIntervalSince(r.serverTime) == 45 * 60)
    }

    @Test func screenTimeResponseLockedChild() throws {
        let json = """
        { "serverTime": "2026-11-07T10:00:00Z", "children": [ { "childId": "c1", "target": "personal", "activeGrants": [], "unlockedUntil": null } ] }
        """
        let r = try decode(ScreenTimeResponse.self, json)
        #expect(r.children[0].unlockedUntil == nil)
    }

}

struct SetupTests {
    @Test func baseURLNormalisation() {
        #expect(BaseURL.parse(" https://opencozy.example.test/manage?x=1 ")?.absoluteString == "https://opencozy.example.test")
        #expect(BaseURL.parse("HTTPS://opencozy.example.test:8443/")?.absoluteString == "https://opencozy.example.test:8443")
        #expect(BaseURL.parse("opencozy.example.test") == nil)
        #expect(BaseURL.parse("ftp://opencozy.example.test") == nil)
    }

    @Test func pinValidation() {
        #expect(ParentPIN.isValid("1234"))
        #expect(!ParentPIN.isValid("123"))
        #expect(!ParentPIN.isValid("12a4"))
        #expect(!ParentPIN.isValid("١٢٣٤"))
    }
}

struct PINThrottleTests {
    @Test func lockoutGrowsAndCaps() {
        #expect(ParentPIN.lockout(afterFailures: 4) == nil)
        #expect(ParentPIN.lockout(afterFailures: 5) == 30)
        #expect(ParentPIN.lockout(afterFailures: 6) == 60)
        #expect(ParentPIN.lockout(afterFailures: 50) == ParentPIN.maxLockout)
    }

    @Test func throttlePersistsAcrossInstancesAndResetsOnSuccess() {
        let keychain = Keychain(service: "org.opencozy.tests.\(UUID().uuidString)")
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        #expect(ParentPIN(keychain: keychain).set("2468"))
        for _ in 0..<4 { #expect(ParentPIN(keychain: keychain).attempt("0000", now: now) == .wrong(lockedUntil: nil)) }
        // A fresh instance (as when the sheet is reopened) sees the same count.
        #expect(ParentPIN(keychain: keychain).attempt("0000", now: now) == .wrong(lockedUntil: now.addingTimeInterval(30)))
        #expect(ParentPIN(keychain: keychain).attempt("2468", now: now.addingTimeInterval(10)) == .throttled(until: now.addingTimeInterval(30)))
        #expect(ParentPIN(keychain: keychain).attempt("2468", now: now.addingTimeInterval(31)) == .success)
        #expect(ParentPIN(keychain: keychain).throttle == ParentPIN.Throttle())
        keychain.set(nil as Data?, for: ParentPIN.account)
    }
}

@MainActor
struct RefreshQueueTests {
    actor Counter {
        var screenTimeCalls = 0
        func hit() -> Int { screenTimeCalls += 1; return screenTimeCalls }
    }

    struct SlowAPI: OpenCozyAPI {
        let fixture = FixtureAPI(unlockedMinutes: nil)
        let counter: Counter
        func device() async throws -> DeviceInfo { try await fixture.device() }
        func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
            _ = await counter.hit()
            try await Task.sleep(for: .milliseconds(200))
            return try await fixture.screenTime(target: target)
        }
    }

    @Test func refreshDuringRefreshQueuesAnotherPass() async throws {
        let counter = Counter()
        let model = AppModel(preview: SlowAPI(counter: counter))
        let first = Task { await model.refresh() }
        try await Task.sleep(for: .milliseconds(50))
        await model.refresh() // returns immediately, but must not be dropped
        await first.value
        #expect(await counter.screenTimeCalls == 2)
        #expect(!model.isRefreshing)
    }
}

@MainActor
struct DeviceRejectionTests {
    struct RejectingAPI: OpenCozyAPI {
        let status: Int
        func device() async throws -> DeviceInfo { throw APIError.server(status: status, message: "Rejected") }
        func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
            try await FixtureAPI(unlockedMinutes: 20).screenTime(target: target)
        }
        func webSession() async throws -> DeviceWebSession { throw APIError.server(status: status, message: "Rejected") }
    }

    @Test(arguments: [401, 403])
    func rejectedSyncLocksImmediately(status: Int) async {
        let model = AppModel(preview: RejectingAPI(status: status))
        await model.refresh()
        #expect(model.accessProblem != nil)
        #expect(model.unlockedUntil == nil)
    }

    @Test(arguments: [401, 403])
    func rejectedWebSessionLocksToo(status: Int) async {
        let model = AppModel(preview: RejectingAPI(status: status), config: WebSessionTests.config())
        await #expect(throws: APIError.self) { try await model.webSessionURL() }
        #expect(model.accessProblem != nil)
        #expect(model.unlockedUntil == nil)
    }

    @Test func otherErrorsDontLock() async {
        let model = AppModel(preview: RejectingAPI(status: 409))
        await model.refresh()
        #expect(model.accessProblem == nil)
    }
}

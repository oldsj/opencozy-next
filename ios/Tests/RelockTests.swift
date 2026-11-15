import Foundation
import Testing
@testable import OpenCozy

/// The relock path (missed-relock review) and "One more minute".
struct RelockDecisionTests {
    let synced = ClockReading(wall: Date(timeIntervalSince1970: 1_800_000_000), monotonic: 5_000, bootSession: "boot-a", timeZone: "UTC")
    func window(_ seconds: TimeInterval, from clock: ClockReading? = nil) -> UnlockState {
        let at = clock ?? synced
        return UnlockState(unlockedUntil: at.wall.addingTimeInterval(seconds), synced: at)
    }

    @Test func intervalStartNeverArms() {
        #expect(LockDecision.decide(state: window(1800), clock: synced.advanced(by: 5), context: .intervalStart, armedAtMonotonic: nil) == .unchanged)
        #expect(LockDecision.decide(state: nil, clock: synced, context: .intervalStart, armedAtMonotonic: nil) == .shield)
        #expect(LockDecision.decide(state: window(30), clock: synced.advanced(by: 40), context: .intervalStart, armedAtMonotonic: nil) == .shield)
    }

    @Test func theLaterOfServerAndBonusCounts() {
        let clock = synced.advanced(by: 10)
        #expect(LockDecision.remaining(state: window(30), bonus: window(60), clock: clock) == 50)
        #expect(LockDecision.remaining(state: window(600), bonus: window(60), clock: clock) == 590)
        #expect(LockDecision.remaining(state: nil, bonus: window(60), clock: clock) == 50)
        #expect(LockDecision.remaining(state: nil, bonus: nil, clock: clock) == nil)
    }

    @Test func bonusSurvivesTheReplacedSchedulesEndCallback() {
        // Arming the minute overwrites the old (ended) grant schedule, whose end fires at once.
        let clock = synced.advanced(by: 2)
        #expect(LockDecision.decide(state: nil, bonus: window(60), clock: clock, context: .intervalEnd, armedAtMonotonic: synced.monotonic)
                == .keepArmed(lockAt: synced.wall.addingTimeInterval(60)))
    }

    @Test func bonusEndShields() {
        let clock = synced.advanced(by: 58)
        #expect(LockDecision.decide(state: nil, bonus: window(60), clock: clock, context: .intervalEnd, armedAtMonotonic: synced.monotonic) == .shield)
    }

    @Test func aCallbackRightAfterArmingWithSecondsLeftStillShields() {
        // Armed 8 s before the end: a callback 3 s later is the real end, not a superseded one.
        let armedAt = synced.advanced(by: 1792)
        #expect(LockDecision.decide(state: window(1800), clock: armedAt.advanced(by: 3), context: .intervalEnd,
                                    armedAtMonotonic: armedAt.monotonic) == .shield)
    }

    @Test func bonusAfterClockChangeShields() {
        var clock = synced.advanced(by: 10)
        clock.wall = clock.wall.addingTimeInterval(3600)
        #expect(LockDecision.decide(state: nil, bonus: window(60), clock: clock, context: .ordinary, armedAtMonotonic: nil) == .shield)
    }

    @Test func backstopEndsAMinuteAfterTheWindow() {
        let now = synced.wall
        let long = MonitorPlan.backstop(now: now, lockAt: now.addingTimeInterval(3600))
        #expect(long.end == now.addingTimeInterval(3660))
        #expect(long.warningMinutes == nil)
        let short = MonitorPlan.backstop(now: now, lockAt: now.addingTimeInterval(60))
        #expect(short.end == now.addingTimeInterval(MonitorPlan.minimumInterval))
    }
}

struct OneMoreMinuteTests {
    let clock = ClockReading(wall: Date(timeIntervalSince1970: 1_800_000_000), monotonic: 5_000, bootSession: "boot-a", timeZone: "UTC")

    func freshStore(_ name: String) -> SharedStore {
        UserDefaults(suiteName: name)?.removePersistentDomain(forName: name)
        return SharedStore(suiteName: name)
    }

    @Test func oncePerLockAndNoChaining() {
        let store = freshStore("one-more-minute-tests")
        #expect(OneMoreMinute.isAvailable(store: store, clock: clock))
        OneMoreMinute.start(store: store, clock: clock)
        #expect(!OneMoreMinute.isAvailable(store: store, clock: clock), "running")
        #expect(store.pendingEvents.map(\.kind) == [.oneMoreMinute])
        // The minute ends and the shield returns: that lock followed no server window.
        OneMoreMinute.shielded(store: store)
        #expect(!OneMoreMinute.isAvailable(store: store, clock: clock.advanced(by: 61)), "no chaining")
        // A real window runs and ends: a fresh lock, so it's available again.
        OneMoreMinute.serverWindowOpened(store: store)
        OneMoreMinute.shielded(store: store)
        #expect(OneMoreMinute.isAvailable(store: store, clock: clock.advanced(by: 3600)))
    }

    @Test func eventsAreAcknowledgedAndBounded() {
        let store = freshStore("one-more-minute-events")
        for index in 0..<60 { store.enqueue(ScreenTimeEvent(id: "e\(index)", kind: .oneMoreMinute, at: clock)) }
        #expect(store.pendingEvents.count == 50)
        #expect(store.pendingEvents.first?.id == "e10")
        store.acknowledge(Set(store.pendingEvents.map(\.id)))
        #expect(store.pendingEvents.isEmpty)
    }

    @Test func secondsAgoUsesTheMonotonicClockInTheSameBoot() {
        let event = ScreenTimeEvent(id: "e", kind: .oneMoreMinute, at: clock)
        var later = clock.advanced(by: 90)
        later.wall = later.wall.addingTimeInterval(3600)
        #expect(event.secondsAgo(now: later) == 90)
        let rebooted = ClockReading(wall: clock.wall.addingTimeInterval(300), monotonic: 10, bootSession: "boot-b", timeZone: "UTC")
        #expect(event.secondsAgo(now: rebooted) == 300)
    }
}

/// The app sends shield events at its next sync and forgets the recorded ones.
@MainActor
@Suite(.serialized)
struct ShieldEventUploadTests {
    actor Recorder {
        var uploads: [[ScreenTimeEventUpload]] = []
        var asks: [String] = []
        func add(_ events: [ScreenTimeEventUpload]) { uploads.append(events) }
        func ask(_ requestId: String) { asks.append(requestId) }
    }

    struct RecordingAPI: OpenCozyAPI {
        let recorder: Recorder
        let accept: Bool
        func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
        func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) {
            try await FixtureAPI().screenTime(target: target)
        }
        func screenTimeEvents(_ events: [ScreenTimeEventUpload]) async throws -> Set<String> {
            await recorder.add(events)
            if !accept { throw APIError.notConfigured }
            return Set(events.map(\.id))
        }
        func askForTime(requestId: String) async throws -> TimeRequestStatus {
            await recorder.ask(requestId)
            if let askFailure { throw askFailure }
            return TimeRequestStatus(id: "00000000-0000-4000-8000-0000000000f0", state: .pending)
        }
        var askFailure: APIError? = nil
    }

    // "Ask for more time" queued on the shield: sent with its request ID, kept
    // only while it may not have reached the server.
    @Test func sendsAQueuedRequestOnce() async {
        let recorder = Recorder()
        let model = AppModel(preview: RecordingAPI(recorder: recorder, accept: true))
        model.lock.store.queuedTimeRequest = nil
        TimeRequestQueue.ask(store: model.lock.store)
        let queued = model.lock.store.queuedTimeRequest
        TimeRequestQueue.ask(store: model.lock.store)
        #expect(model.lock.store.queuedTimeRequest == queued, "a second tap keeps the first request ID")
        await model.refresh()
        #expect(await recorder.asks == [queued?.requestId ?? "missing"])
        #expect(model.lock.store.queuedTimeRequest == nil)
        await model.refresh()
        #expect(await recorder.asks.count == 1)
    }

    @Test func keepsARequestThatMayNotHaveArrived() async {
        let recorder = Recorder()
        let model = AppModel(preview: RecordingAPI(recorder: recorder, accept: true, askFailure: .transport("offline")))
        model.lock.store.queuedTimeRequest = nil
        TimeRequestQueue.ask(store: model.lock.store)
        await model.refresh()
        #expect(model.lock.store.queuedTimeRequest != nil)
        #expect(model.syncFailed)
        model.lock.store.queuedTimeRequest = nil
    }

    @Test func dropsARequestAParentJustRefused() async {
        let recorder = Recorder()
        let model = AppModel(preview: RecordingAPI(recorder: recorder, accept: true, askFailure: .server(status: 409, message: "A parent just said no.")))
        model.lock.store.queuedTimeRequest = nil
        TimeRequestQueue.ask(store: model.lock.store)
        await model.refresh()
        #expect(model.lock.store.queuedTimeRequest == nil)
        #expect(!model.syncFailed)
    }

    @Test func uploadsThenForgets() async {
        let recorder = Recorder()
        let model = AppModel(preview: RecordingAPI(recorder: recorder, accept: true))
        model.lock.store.pendingEvents = []
        let at = ClockReading.now().advanced(by: -30)
        model.lock.store.enqueue(ScreenTimeEvent(id: "00000000-0000-4000-8000-0000000000e1", kind: .oneMoreMinute, at: at))
        await model.refresh()
        let uploads = await recorder.uploads
        #expect(uploads.count == 1)
        #expect(uploads.first?.first?.kind == "one_more_minute")
        #expect((29...31).contains(uploads.first?.first?.secondsAgo ?? -1))
        #expect(model.lock.store.pendingEvents.isEmpty)
    }

    @Test func keepsEventsWhenTheUploadFails() async {
        let recorder = Recorder()
        let model = AppModel(preview: RecordingAPI(recorder: recorder, accept: false))
        model.lock.store.pendingEvents = []
        model.lock.store.enqueue(ScreenTimeEvent(id: "00000000-0000-4000-8000-0000000000e2", kind: .oneMoreMinute, at: .now()))
        await model.refresh()
        #expect(model.lock.store.pendingEvents.count == 1)
        #expect(model.syncFailed)
        model.lock.store.pendingEvents = []
    }
}

@Suite(.serialized)
@MainActor
struct KidPushTokenTests {
    actor Tokens { var sent: [PushTokenRequest] = []; func add(_ r: PushTokenRequest) { sent.append(r) } }
    struct TokenAPI: OpenCozyAPI {
        let tokens: Tokens
        func device() async throws -> DeviceInfo { try await FixtureAPI().device() }
        func screenTime(target: ScreenTimeTarget) async throws -> (ScreenTimeResponse, receivedAt: Date) { try await FixtureAPI().screenTime(target: target) }
        func registerPushToken(_ request: PushTokenRequest) async throws { await tokens.add(request) }
    }

    @Test func registersTheTokenOnce() async {
        let tokens = Tokens()
        let model = AppModel(preview: TokenAPI(tokens: tokens))
        await model.didReceivePushToken(Data([0xab, 0x01]))
        await model.refresh()
        let sent = await tokens.sent
        #expect(sent.map(\.token) == ["ab01"])
    }
}

import Foundation
import Testing
@testable import OpenCozy

/// The shield's subtitle, composed from the last sync.
struct ShieldTextTests {
    let zone = TimeZone(identifier: "America/New_York")!
    /// 2026-03-02 19:30 local.
    var now: Date { date("2026-03-03T00:30:00Z") }

    func date(_ text: String) -> Date { try! Date(text, strategy: .iso8601) }

    @Test func fallsBackToTheDefaultBeforeAnySync() {
        #expect(ShieldText.subtitle(nil, now: now, timeZone: zone) == ShieldText.defaultMessage)
        #expect(ShieldText.subtitle(ShieldContent(message: "  "), now: now, timeZone: zone) == ShieldText.defaultMessage)
    }

    @Test func showsTheHouseholdMessage() {
        #expect(ShieldText.subtitle(ShieldContent(message: "Read a book first."), now: now, timeZone: zone) == "Read a book first.")
    }

    @Test func openWindowWithBankedTime() {
        let content = ShieldContent(message: "Locked.", bankedSeconds: 45 * 60 + 30, closesAt: date("2026-03-03T01:00:00Z"))
        #expect(ShieldText.subtitle(content, now: now, timeZone: zone) == "Locked.\n\nYou have 45 min saved. Open OpenCozy to use it.")
    }

    @Test func closedUntilTomorrow() {
        let content = ShieldContent(message: "Locked.", bankedSeconds: 600, opensAt: date("2026-03-03T13:00:00Z"))
        #expect(ShieldText.subtitle(content, now: now, timeZone: zone) == "Locked.\n\nScreen time starts at 8:00 AM tomorrow.\n\nYou have 10 min saved.")
    }

    @Test func closedUntilLaterToday() {
        let content = ShieldContent(message: "Locked.", opensAt: date("2026-03-03T01:15:00Z"))
        #expect(ShieldText.subtitle(content, now: now, timeZone: zone) == "Locked.\n\nScreen time starts at 8:15 PM.")
    }

    @Test func aPassedOpeningIsDropped() {
        let content = ShieldContent(message: "Locked.", bankedSeconds: 120, opensAt: date("2026-03-03T00:00:00Z"))
        #expect(ShieldText.subtitle(content, now: now, timeZone: zone) == "Locked.\n\nYou have 2 min saved. Open OpenCozy to use it.")
    }

    @Test func underAMinuteIsNotMentioned() {
        #expect(ShieldText.subtitle(ShieldContent(message: "Locked.", bankedSeconds: 59), now: now, timeZone: zone) == "Locked.")
    }

    @Test func storesThroughTheAppGroup() {
        let store = SharedStore(suiteName: "shield-text-tests")
        store.shieldContent = ShieldContent(message: "Hi", bankedSeconds: 60)
        #expect(store.shieldContent == ShieldContent(message: "Hi", bankedSeconds: 60))
        store.shieldContent = nil
        #expect(store.shieldContent == nil)
        #expect(SharedStore(suiteName: nil).shieldContent == nil)
    }

    @Test func asksForMoreTime() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let base = ShieldContent(message: "Locked.")
        #expect(ShieldText.subtitle(base, now: now, queued: true) == "Locked.\n\nYou asked a parent for more time. Waiting for an answer.")
        #expect(ShieldText.subtitle(base, now: now, queued: true, canOpenApp: false) == "Locked.\n\nOpen OpenCozy to send it.")
        var pending = base; pending.ask = .pending
        #expect(ShieldText.subtitle(pending, now: now) == "Locked.\n\nYou asked a parent for more time. Waiting for an answer.")
        var declined = base; declined.ask = .declined
        #expect(ShieldText.subtitle(declined, now: now) == "Locked.\n\nA parent said not right now. You can ask again in a few minutes.")
        #expect(ShieldText.subtitle(nil, now: now, queued: true) == "\(ShieldText.defaultMessage)\n\nYou asked a parent for more time. Waiting for an answer.")
        #expect(ShieldText.askButton(nil, queued: false) == "Ask for more time")
        #expect(ShieldText.askButton(.pending, queued: false) == "Asked for more time")
        #expect(ShieldText.askButton(.declined, queued: true) == "Asked for more time")
    }

    /// While an ask applies, its status takes the saved line's place.
    @Test func askReplacesSavedTime() {
        var content = ShieldContent(message: "Locked.", bankedSeconds: 12 * 60)
        #expect(ShieldText.subtitle(content, now: now, timeZone: zone) == "Locked.\n\nYou have 12 min saved. Open OpenCozy to use it.")
        content.ask = .pending
        #expect(ShieldText.subtitle(content, now: now, timeZone: zone) == "Locked.\n\nYou asked a parent for more time. Waiting for an answer.")
        content.ask = nil
        #expect(ShieldText.subtitle(content, now: now, queued: true, canOpenApp: false, timeZone: zone) == "Locked.\n\nOpen OpenCozy to send it.")
    }

    @Test func timePushes() {
        let pending = TimePush(userInfo: ["opencozy": ["type": "time.pending", "requestId": "r1", "requestVersion": 1, "pendingCount": 2]])
        #expect(pending == TimePush(requestId: "r1", version: 1, pendingCount: 2))
        #expect(TimePush(userInfo: ["opencozy": ["type": "time.resolved", "requestId": "r1", "state": "approved"]]) == TimePush(requestId: "r1", version: nil))
        #expect(TimePush(userInfo: ["opencozy": ["type": "approval.pending", "approvalId": "a1", "approvalVersion": 1]]) == nil)
        #expect(ApprovalPush(userInfo: ["opencozy": ["type": "time.pending", "requestId": "r1", "requestVersion": 1]]) == nil)
        #expect(TimeChoice.allCases.map(\.wire) == ["30min", "1h", "all_day", "no"])
    }
}

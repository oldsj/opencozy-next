import Foundation
import Testing
@testable import OpenCozy

/// The iPad's web filter: game portals always, games only outside the window.
struct WebFilterTests {
    let lists = WebFilterLists(gamePortals: ["poki.example", "crazy.example"], games: ["kids-games.example", "Poki.example"])

    @Test func portalsAlwaysGamesOnlyOutsideTheWindow() {
        #expect(WebFilterPlan.plan(lists, windowOpen: false).domains == ["poki.example", "crazy.example", "kids-games.example"])
        #expect(WebFilterPlan.plan(lists, windowOpen: true).domains == ["poki.example", "crazy.example"])
    }

    @Test func noListsBlockNothing() {
        #expect(WebFilterPlan.plan(nil, windowOpen: false) == .init(domains: [], dropped: 0))
        #expect(WebFilterPlan.plan(WebFilterLists(), windowOpen: false).domains.isEmpty)
        #expect(WebFilterPlan.plan(WebFilterLists(games: ["kids-games.example"]), windowOpen: true).domains.isEmpty)
    }

    @Test func overTheLimitKeepsPortalsFirst() {
        let many = WebFilterLists(gamePortals: (0..<45).map { "p\($0).example" }, games: (0..<10).map { "g\($0).example" })
        let plan = WebFilterPlan.plan(many, windowOpen: false)
        #expect(plan.domains.count == WebFilterPlan.maxDomains)
        #expect(plan.dropped == 5)
        #expect(plan.domains.prefix(45) == ArraySlice((0..<45).map { "p\($0).example" }))
    }

    @Test func listsRoundTripThroughTheAppGroup() {
        let store = SharedStore(suiteName: "opencozy.tests.\(UUID().uuidString)")
        #expect(store.webFilterLists == nil)
        store.webFilterLists = lists
        #expect(store.webFilterLists == lists)
        store.webFilterLists = nil
        #expect(store.webFilterLists == nil)
    }

    @Test func screenTimeResponseDecodesTheLists() throws {
        let json = #"{"serverTime":"2026-11-14T12:00:00.000Z","children":[],"webFilter":{"gamePortals":["poki.example"],"games":["kids-games.example"]}}"#
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            try Date(try decoder.singleValueContainer().decode(String.self), strategy: .iso8601.year().month().day().time(includingFractionalSeconds: true))
        }
        let response = try decoder.decode(ScreenTimeResponse.self, from: Data(json.utf8))
        #expect(response.webFilter == WebFilterLists(gamePortals: ["poki.example"], games: ["kids-games.example"]))
        let older = try decoder.decode(ScreenTimeResponse.self, from: Data(#"{"serverTime":"2026-11-14T12:00:00.000Z","children":[]}"#.utf8))
        #expect(older.webFilter == nil)
    }
}

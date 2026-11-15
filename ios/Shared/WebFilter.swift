import Foundation

/// The household's blocked websites, from the last screen-time sync. Browsers
/// are otherwise open: game portals are blocked always, games only outside the
/// child's window. Lowercase domain names; the server validates them.
public struct WebFilterLists: Codable, Equatable, Sendable {
    public var gamePortals: [String]
    public var games: [String]

    public init(gamePortals: [String] = [], games: [String] = []) {
        self.gamePortals = gamePortals
        self.games = games
    }
}

/// What the iPad's web filter blocks, decided from the lists and whether the
/// child's window is open. Pure, so it is unit-tested without ManagedSettings.
public enum WebFilterPlan {
    /// Apple's specific-domain filter takes a limited list; portals come first so
    /// the always-blocked list is the last to be cut.
    public static let maxDomains = 50

    public struct Plan: Equatable, Sendable {
        /// The domains to block, or empty for no filter.
        public var domains: [String]
        /// How many domains did not fit.
        public var dropped: Int
    }

    public static func plan(_ lists: WebFilterLists?, windowOpen: Bool) -> Plan {
        guard let lists else { return Plan(domains: [], dropped: 0) }
        var seen = Set<String>(), ordered: [String] = []
        for domain in lists.gamePortals + (windowOpen ? [] : lists.games) {
            let name = domain.lowercased()
            if !name.isEmpty, seen.insert(name).inserted { ordered.append(name) }
        }
        return Plan(domains: Array(ordered.prefix(maxDomains)), dropped: max(0, ordered.count - maxDomains))
    }
}

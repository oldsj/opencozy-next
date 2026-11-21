import Foundation
import UserNotifications

/// The `opencozy` part of an inbox push (internal/push/notification.go): `approval.pending`
/// when something enters the inbox (category APPROVAL, or TIME_REQUEST for "more time"), and the
/// silent `approval.resolved` when it leaves it anywhere.
struct ApprovalPush: Equatable, Sendable {
    enum Event: Equatable, Sendable {
        case pending
        /// Left the inbox (approved, rejected, answered or withdrawn anywhere).
        case resolved
    }

    /// The inbox item's id (`itemId`).
    var approvalId: String
    var event: Event
    /// The item's kind (`completion`, `routine`, `redemption`, `time_request`); nil when resolved.
    var kind: String?
    var pendingCount: Int?

    init(approvalId: String, event: Event, kind: String? = nil, pendingCount: Int? = nil) {
        self.approvalId = approvalId
        self.event = event
        self.kind = kind
        self.pendingCount = pendingCount
    }

    init?(userInfo: [AnyHashable: Any]) {
        guard let body = userInfo["opencozy"] as? [String: Any],
              let id = body["itemId"] as? String, !id.isEmpty,
              let type = body["type"] as? String else { return nil }
        switch type {
        case "approval.pending": event = .pending
        case "approval.resolved": event = .resolved
        default: return nil
        }
        approvalId = id
        kind = body["kind"] as? String
        pendingCount = (body["pendingCount"] as? NSNumber)?.intValue
    }

    /// A child's "Ask for more time": answered with a `TimeChoice`, not Approve/Reject.
    var isTimeRequest: Bool { kind == "time_request" }
}

/// The TIME_REQUEST actions, and the answer each sends.
enum TimeChoice: String, CaseIterable, Sendable {
    case thirtyMinutes = "TIME_30"
    case oneHour = "TIME_60"
    case allDay = "TIME_ALL_DAY"
    case no = "TIME_NO"

    var wire: String {
        switch self {
        case .thirtyMinutes: "30min"
        case .oneHour: "1h"
        case .allDay: "all_day"
        case .no: "no"
        }
    }

    var title: String {
        switch self {
        case .thirtyMinutes: "30 min"
        case .oneHour: "1 hour"
        case .allDay: "All day"
        case .no: "No"
        }
    }
}

/// What a notification action asks for.
enum ApprovalDecision: String, Sendable {
    case approve = "APPROVE"
    case reject = "REJECT"
}

enum ApprovalNotifications {
    static let category = "APPROVAL"
    static let timeCategory = "TIME_REQUEST"

    /// The actions run without unlocking the iPhone, so they work from an Apple Watch (which is
    /// itself unlocked on the parent's wrist). Parents hide them on a locked iPhone with
    /// Settings → Notifications → OpenCozy → Show Previews → When Unlocked.
    static func register(on center: UNUserNotificationCenter = .current()) {
        let approve = UNNotificationAction(identifier: ApprovalDecision.approve.rawValue, title: "Approve",
                                           options: [])
        let reject = UNNotificationAction(identifier: ApprovalDecision.reject.rawValue, title: "Reject",
                                          options: [.destructive])
        let time = TimeChoice.allCases.map { choice in
            UNNotificationAction(identifier: choice.rawValue, title: choice.title,
                                 options: choice == .no ? [.destructive] : [])
        }
        center.setNotificationCategories([
            UNNotificationCategory(identifier: category, actions: [approve, reject], intentIdentifiers: [], options: []),
            UNNotificationCategory(identifier: timeCategory, actions: time, intentIdentifiers: [], options: []),
        ])
    }

    /// Removes delivered notifications for items no longer pending, and sets the badge.
    static func sync(pendingIds: Set<String>, pendingCount: Int, center: UNUserNotificationCenter = .current()) async {
        let delivered = await center.deliveredNotifications()
        let stale = delivered.filter { note in
            guard let push = ApprovalPush(userInfo: note.request.content.userInfo) else { return false }
            return !pendingIds.contains(push.approvalId)
        }.map(\.request.identifier)
        if !stale.isEmpty { center.removeDeliveredNotifications(withIdentifiers: stale) }
        try? await center.setBadgeCount(pendingCount)
    }

    /// Removes delivered notifications for one inbox item.
    static func remove(approvalId: String, center: UNUserNotificationCenter = .current()) async {
        let ids = await center.deliveredNotifications()
            .filter { ApprovalPush(userInfo: $0.request.content.userInfo)?.approvalId == approvalId }
            .map(\.request.identifier)
        if !ids.isEmpty { center.removeDeliveredNotifications(withIdentifiers: ids) }
    }

    static func setBadge(_ count: Int, center: UNUserNotificationCenter = .current()) async {
        try? await center.setBadgeCount(max(0, count))
    }

    /// Tells the parent what happened to a notification action.
    static func post(title: String, body text: String, center: UNUserNotificationCenter = .current()) async {
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = text
        content.threadIdentifier = "opencozy-approvals"
        try? await center.add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
    }
}

/// Notification-center side effects of the parent inbox; replaced in tests.
struct ApprovalNotifier: Sendable {
    /// Remove delivered notifications for items not in the set, and set the badge.
    var sync: @MainActor @Sendable (Set<String>, Int) async -> Void
    /// Remove delivered notifications for one approval.
    var remove: @MainActor @Sendable (String) async -> Void
    var setBadge: @MainActor @Sendable (Int) async -> Void
    /// Post a local notification: title, body.
    var post: @MainActor @Sendable (String, String) async -> Void

    static let system = ApprovalNotifier(
        sync: { await ApprovalNotifications.sync(pendingIds: $0, pendingCount: $1) },
        remove: { await ApprovalNotifications.remove(approvalId: $0) },
        setBadge: { await ApprovalNotifications.setBadge($0) },
        post: { await ApprovalNotifications.post(title: $0, body: $1) })

    static let none = ApprovalNotifier(sync: { _, _ in }, remove: { _ in }, setBadge: { _ in }, post: { _, _ in })
}

extension Data {
    /// APNs device token as the lowercase hex the server expects.
    var hexString: String { map { String(format: "%02x", $0) }.joined() }
}

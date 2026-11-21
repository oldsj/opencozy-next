import UIKit
import UserNotifications

/// Owns the per-role models and the notification plumbing. The models are lazy, so
/// a parent device never creates `AppModel` (which reads Family Controls state).
@MainActor
final class AppDelegate: NSObject, UIApplicationDelegate, @preconcurrency UNUserNotificationCenterDelegate {
    let roles = RoleStore()
    lazy var kid = AppModel()
    lazy var parent = ParentModel()

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        // The delegate must be set before launch finishes to receive action responses
        // that launched the app.
        // Drop any Phase-1 bearer key left by an upgrade, whatever the role.
        AppConfig().purgeLegacyCredential()
        UNUserNotificationCenter.current().delegate = self
        ApprovalNotifications.register()
        if roles.role == .parent, parent.isSignedIn {
            Self.registerForPush()
        }
        // A child's paired device: pushes when a TV waits, TV time is almost up,
        // or a parent answers "Ask for more time".
        if roles.role == .kid, kid.isPaired {
            Self.registerForPush()
        }
        return true
    }

    /// Asks for alert permission and registers with APNs. Registration works even if
    /// alerts are declined (badge updates still arrive).
    static func registerForPush() {
        Task { @MainActor in
            _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound])
            UIApplication.shared.registerForRemoteNotifications()
        }
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        if roles.role == .kid { Task { await kid.didReceivePushToken(deviceToken) } }
        guard roles.role == .parent else { return }
        Task { await parent.didReceivePushToken(deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        // Simulator or no push entitlement; the inbox still works by refresh.
    }

    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any]) async -> UIBackgroundFetchResult {
        // Kid pushes: sync, so a parent's yes lifts the shields at once.
        if roles.role == .kid {
            await kid.refresh()
            return .newData
        }
        guard roles.role == .parent, let push = ApprovalPush(userInfo: userInfo) else { return .noData }
        // Background push (content-available): clear a resolved item's notification
        // first, since the refresh needs the home network.
        await parent.handleResolvedPush(push)
        await parent.refresh()
        return .newData
    }

    // MARK: UNUserNotificationCenterDelegate

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        if roles.role == .kid {
            await kid.refresh()
            return [.banner, .sound, .list]
        }
        guard roles.role == .parent, let push = ApprovalPush(userInfo: notification.request.content.userInfo) else { return [] }
        await parent.handleResolvedPush(push)
        await parent.refresh()
        return push.event == .resolved ? [.badge] : [.banner, .sound, .badge, .list]
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        if roles.role == .kid {
            await kid.refresh()
            return
        }
        guard roles.role == .parent, let push = ApprovalPush(userInfo: response.notification.request.content.userInfo) else { return }
        if let choice = TimeChoice(rawValue: response.actionIdentifier) {
            await parent.handleTimeAction(choice, push: push)
        } else if let decision = ApprovalDecision(rawValue: response.actionIdentifier) {
            await parent.handleAction(decision, push: push)
        } else {
            await parent.refresh()
        }
    }
}

@MainActor
@Observable
final class RoleStore {
    var role: DeviceRole? = DeviceRole.load() {
        didSet {
            DeviceRole.save(role)
            // A signed-in web session must never carry over into another role.
            if role != oldValue { WebDataGate.shared.reset() }
        }
    }
}

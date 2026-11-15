import ManagedSettings

/// Handles the shield's buttons.
///
/// - Primary: **One more minute** when available (once per lock that follows a
///   real window; `OneMoreMinute`) lifts the shield for 60 seconds, offline, and
///   queues the event for the app to send. Otherwise **OK** closes the locked app.
/// - Secondary: **Ask for more time**. This extension cannot sign device requests
///   (the Secure Enclave key belongs to the app) and Apple documents no network
///   use here, so the request is queued in the App Group. On iOS 26.5+ the
///   response opens OpenCozy, which sends it at once; earlier, the shield asks
///   the child to open OpenCozy, and any later sync sends it.
final class ShieldActionExtension: ShieldActionDelegate {
    private let lock = ScreenTimeLock()

    override func handle(action: ShieldAction, for application: ApplicationToken, completionHandler: @escaping (ShieldActionResponse) -> Void) {
        completionHandler(response(to: action))
    }

    override func handle(action: ShieldAction, for webDomain: WebDomainToken, completionHandler: @escaping (ShieldActionResponse) -> Void) {
        completionHandler(response(to: action))
    }

    override func handle(action: ShieldAction, for category: ActivityCategoryToken, completionHandler: @escaping (ShieldActionResponse) -> Void) {
        completionHandler(response(to: action))
    }

    private func response(to action: ShieldAction) -> ShieldActionResponse {
        switch action {
        case .primaryButtonPressed:
            // `.defer` keeps the app in front: with the shield lifted the child carries on.
            if case .unlocked = lock.useOneMoreMinute() { return .defer }
            return .close
        case .secondaryButtonPressed:
            TimeRequestQueue.ask(store: lock.store)
            if #available(iOS 26.5, *) { return .openParentalControlsApp }
            // `.defer` redraws the shield, which now says how to send the request.
            return .defer
        case .firstSecondarySubmenuItemPressed, .secondSecondarySubmenuItemPressed, .thirdSecondarySubmenuItemPressed:
            // The shield has no submenu (ShieldConfigurationExtension sets none).
            return .close
        @unknown default:
            return .close
        }
    }
}

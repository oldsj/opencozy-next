import Foundation

/// Where the web view may go. Pure, so it's tested without WebKit.
enum WebPolicy {
    enum Decision: Equatable {
        /// Load it in the web view.
        case allow
        /// Hand it to the system (Safari, Mail, Phone).
        case openExternally
        /// Drop it.
        case cancel
    }

    /// The web view stays on the canonical origin. Links elsewhere open in Safari,
    /// `mailto:`/`tel:` go to the system, and anything else is dropped. Frames from
    /// other origins are refused too: the web app embeds none.
    static func decide(_ url: URL?, origin: URL, isMainFrame: Bool) -> Decision {
        guard let url, let scheme = url.scheme?.lowercased() else { return .cancel }
        switch scheme {
        case "about":
            return .allow
        case "http", "https":
            if isSameOrigin(url, origin) { return .allow }
            return isMainFrame ? .openExternally : .cancel
        case "mailto", "tel", "sms":
            return isMainFrame ? .openExternally : .cancel
        case "blob", "data":
            return isMainFrame ? .cancel : .allow
        default:
            return .cancel
        }
    }

    /// Same scheme, host and port (defaults filled in).
    static func isSameOrigin(_ url: URL, _ origin: URL) -> Bool {
        guard let a = Origin(url), let b = Origin(origin) else { return false }
        return a == b
    }

    /// On a kid device, these pages mean the child web session has ended
    /// (missing, expired, revoked or reassigned) and a new exchange is needed:
    /// `/device/expired` is the server's signal, and `/login` is never right for a
    /// child, who doesn't sign in by hand.
    static func isSessionEndedPage(_ url: URL?, origin: URL) -> Bool {
        guard let url, isSameOrigin(url, origin) else { return false }
        let path = url.path(percentEncoded: false)
        return ["/device/expired", "/login"].contains { path == $0 || path.hasPrefix($0 + "/") }
    }

    private struct Origin: Equatable {
        var scheme: String
        var host: String
        var port: Int

        init?(_ url: URL) {
            guard let scheme = url.scheme?.lowercased(), scheme == "https" || scheme == "http",
                  let host = url.host()?.lowercased(), !host.isEmpty else { return nil }
            self.scheme = scheme
            self.host = host
            self.port = url.port ?? (scheme == "https" ? 443 : 80)
        }
    }
}

/// When the parent web view should try `POST /api/phone/sessions/web`: on the first
/// page that isn't the sign-in page (the cookie may survive from last time), and
/// each time the page leaves `/login` (the web sign-in just finished). Not on
/// every navigation, so a signed-out web view doesn't call it repeatedly.
struct WebSignInWatcher {
    let origin: URL
    private var sawPage = false
    private var onLogin = false

    init(origin: URL) { self.origin = origin }

    mutating func shouldTry(after url: URL?) -> Bool {
        guard let url, WebPolicy.isSameOrigin(url, origin) else { return false }
        let path = url.path(percentEncoded: false)
        let isLogin = path == "/login" || path.hasPrefix("/login/")
        defer { onLogin = isLogin; if !isLogin { sawPage = true } }
        if isLogin { return false }
        return !sawPage || onLogin
    }
}

/// Messages the page may send to the app through
/// `window.webkit.messageHandlers.opencozy.postMessage(…)`. Deliberately narrow
/// (contract "Native bridge"): anything else is ignored.
enum ShellMessage: Equatable {
    /// A screen-time redemption happened in the page: re-sync shields now.
    case screenTimeChanged

    static let handlerName = "opencozy"

    init?(body: Any) {
        guard let dict = body as? [String: Any], let type = dict["type"] as? String else { return nil }
        switch type {
        case "screenTimeChanged": self = .screenTimeChanged
        default: return nil
        }
    }
}

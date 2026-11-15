import SwiftUI
import UIKit
import WebKit

/// One WKWebView on the canonical OpenCozy origin, owned outside SwiftUI's view
/// lifetime so a re-render never reloads the page.
///
/// - Cookies persist: the default (persistent) website data store, shared by every
///   web view in the app. WebKit shares one process pool per app since iOS 15.
/// - Navigation stays on the origin (`WebPolicy`); other links open in Safari.
/// - A load that can't reach the server shows the offline state instead of a
///   blank page; pull to refresh retries.
/// - The page may post `{type: "screenTimeChanged"}` to `opencozy` (main frame,
///   canonical origin only); nothing else crosses the bridge.
@MainActor
@Observable
final class WebShell: NSObject {
    enum State: Equatable {
        case idle
        /// First load in progress (nothing to show yet).
        case loading
        case ready
        case unreachable
    }

    static let unreachableMessage = "Can't reach OpenCozy. Are you home or on the VPN?"

    let origin: URL
    private(set) var state: State = .idle

    /// A bridge message from the page.
    @ObservationIgnored var onMessage: (ShellMessage) -> Void = { _ in }
    /// A main-frame page finished loading.
    @ObservationIgnored var onPageLoaded: () -> Void = {}
    /// Pull to refresh, and Try again in the offline state. Defaults to reloading.
    @ObservationIgnored var onRefresh: (() -> Void)?
    /// Set on a kid device: called (and the navigation dropped) when the main frame
    /// reaches `/device/expired` or `/login`, or gets a 401, meaning the child
    /// session ended. Client-side route changes are caught by watching the URL.
    @ObservationIgnored var onSignedOut: (() -> Void)?
    /// The page now showing, once it's there: after a full load finishes, and on a
    /// client-side route change (`router.push`, which never reaches the navigation
    /// delegate). Never during a load, when the web view may still be on its blank
    /// initial document.
    @ObservationIgnored var onURLChange: (URL?) -> Void = { _ in }
    @ObservationIgnored var openExternally: (URL) -> Void = { UIApplication.shared.open($0) }

    @ObservationIgnored private var lastRequested: URL?
    @ObservationIgnored private var urlObservation: NSKeyValueObservation?
    @ObservationIgnored private var webViewStorage: WKWebView?

    init(origin: URL) {
        self.origin = origin
    }

    var webView: WKWebView {
        if let webViewStorage { return webViewStorage }
        let made = makeWebView()
        webViewStorage = made
        return made
    }

    func load(_ url: URL) {
        lastRequested = url
        if state != .ready { state = .loading }
        webView.load(URLRequest(url: url))
    }

    /// Reloads the current page, or repeats the last load if it never arrived.
    func reload() {
        if state == .unreachable || webView.url == nil, let lastRequested {
            load(lastRequested)
        } else {
            webView.reload()
        }
    }

    func refresh() {
        if let onRefresh { onRefresh() } else { reload() }
    }

    private func makeWebView() -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        configuration.applicationNameForUserAgent = "OpenCozy-iOS"
        configuration.allowsInlineMediaPlayback = true
        configuration.userContentController.add(MessageProxy(self), contentWorld: .page, name: ShellMessage.handlerName)
        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        webView.isOpaque = false
        webView.backgroundColor = .systemBackground
        // Opt-in only (not every Debug build): household installs come from Xcode.
        // Add `SWIFT_ACTIVE_COMPILATION_CONDITIONS = $(inherited) OPENCOZY_WEB_INSPECTOR`
        // to Local.xcconfig to attach Safari's Web Inspector.
        #if OPENCOZY_WEB_INSPECTOR
        webView.isInspectable = true
        #endif
        // history.pushState (a client-side route change) never reaches the
        // navigation delegate, but it does change `url`.
        urlObservation = webView.observe(\.url, options: [.new]) { [weak self] webView, _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                if !webView.isLoading { self.onURLChange(webView.url) }
                if let onSignedOut = self.onSignedOut, WebPolicy.isSessionEndedPage(webView.url, origin: self.origin) { onSignedOut() }
            }
        }
        let refresh = UIRefreshControl()
        refresh.addAction(UIAction { [weak self] _ in self?.refresh() }, for: .valueChanged)
        webView.scrollView.refreshControl = refresh
        return webView
    }

    /// Every cookie in the web view's store (HttpOnly included), for the native
    /// parent-session call. Never handed to page JavaScript.
    func cookies() async -> [HTTPCookie] {
        await webView.configuration.websiteDataStore.httpCookieStore.allCookies()
    }

    private func endRefreshing() {
        webViewStorage?.scrollView.refreshControl?.endRefreshing()
    }

    fileprivate func receive(_ message: WKScriptMessage) {
        guard message.frameInfo.isMainFrame, isOwnOrigin(message.frameInfo.securityOrigin),
              let parsed = ShellMessage(body: message.body) else { return }
        onMessage(parsed)
    }

    private func isOwnOrigin(_ securityOrigin: WKSecurityOrigin) -> Bool {
        var components = URLComponents()
        components.scheme = securityOrigin.protocol
        components.host = securityOrigin.host
        if securityOrigin.port != 0 { components.port = securityOrigin.port }
        return components.url.map { WebPolicy.isSameOrigin($0, origin) } ?? false
    }

    /// Cancellations (ours, or a newer navigation replacing this one) aren't failures.
    nonisolated static func isCancellation(_ error: Error) -> Bool {
        let error = error as NSError
        return (error.domain == NSURLErrorDomain && error.code == NSURLErrorCancelled)
            // WebKitErrorFrameLoadInterruptedByPolicyChange: we cancelled it in a policy decision.
            || (error.domain == "WebKitErrorDomain" && error.code == 102)
    }
}

extension WebShell: WKNavigationDelegate {
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        let url = action.request.url
        // A nil target frame is a new window (target=_blank or window.open).
        let isMainFrame = action.targetFrame?.isMainFrame ?? true
        switch WebPolicy.decide(url, origin: origin, isMainFrame: isMainFrame) {
        case .cancel:
            return .cancel
        case .openExternally:
            if let url { openExternally(url) }
            return .cancel
        case .allow:
            if isMainFrame, let onSignedOut, WebPolicy.isSessionEndedPage(url, origin: origin) {
                onSignedOut()
                return .cancel
            }
            if action.targetFrame == nil, let url {
                webView.load(URLRequest(url: url))
                return .cancel
            }
            return .allow
        }
    }

    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse) async -> WKNavigationResponsePolicy {
        if response.isForMainFrame, let onSignedOut, (response.response as? HTTPURLResponse)?.statusCode == 401 {
            onSignedOut()
            return .cancel
        }
        return .allow
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        state = .ready
        endRefreshing()
        onPageLoaded()
        onURLChange(webView.url)
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    private func failed(_ error: Error) {
        endRefreshing()
        guard !Self.isCancellation(error) else {
            if state == .loading { state = .idle }
            return
        }
        state = .unreachable
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        reload()
    }
}

extension WebShell: WKUIDelegate {
    /// window.open: same rules as a link; never a second web view.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for action: WKNavigationAction,
                 windowFeatures: WKWindowFeatures) -> WKWebView? {
        switch WebPolicy.decide(action.request.url, origin: origin, isMainFrame: true) {
        case .allow: webView.load(action.request)
        case .openExternally: action.request.url.map(openExternally)
        case .cancel: break
        }
        return nil
    }

    // Without these, WebKit silently answers alert/confirm with nothing/false, and
    // the web app's "Archive …?" confirmations could never be accepted.

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo) async {
        await withCheckedContinuation { (done: CheckedContinuation<Void, Never>) in
            let alert = UIAlertController(title: nil, message: message, preferredStyle: .alert)
            alert.addAction(UIAlertAction(title: "OK", style: .default) { _ in done.resume() })
            if !present(alert, from: webView) { done.resume() }
        }
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo) async -> Bool {
        await withCheckedContinuation { (done: CheckedContinuation<Bool, Never>) in
            let alert = UIAlertController(title: nil, message: message, preferredStyle: .alert)
            alert.addAction(UIAlertAction(title: "Cancel", style: .cancel) { _ in done.resume(returning: false) })
            alert.addAction(UIAlertAction(title: "OK", style: .default) { _ in done.resume(returning: true) })
            if !present(alert, from: webView) { done.resume(returning: false) }
        }
    }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String, defaultText: String?,
                 initiatedByFrame frame: WKFrameInfo) async -> String? {
        await withCheckedContinuation { (done: CheckedContinuation<String?, Never>) in
            let alert = UIAlertController(title: nil, message: prompt, preferredStyle: .alert)
            alert.addTextField { $0.text = defaultText }
            alert.addAction(UIAlertAction(title: "Cancel", style: .cancel) { _ in done.resume(returning: nil) })
            alert.addAction(UIAlertAction(title: "OK", style: .default) { [weak alert] _ in done.resume(returning: alert?.textFields?.first?.text) })
            if !present(alert, from: webView) { done.resume(returning: nil) }
        }
    }

    private func present(_ alert: UIAlertController, from webView: WKWebView) -> Bool {
        guard var top = webView.window?.rootViewController else { return false }
        while let presented = top.presentedViewController { top = presented }
        top.present(alert, animated: true)
        return true
    }
}

/// WKUserContentController retains its handlers; this keeps the shell releasable.
private final class MessageProxy: NSObject, WKScriptMessageHandler {
    weak var shell: WebShell?
    init(_ shell: WebShell) { self.shell = shell }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        MainActor.assumeIsolated { shell?.receive(message) }
    }
}

/// Shows the shell's web view, with the offline state over it when the server
/// can't be reached.
struct WebShellView: View {
    let shell: WebShell

    var body: some View {
        ZStack {
            WebViewHost(webView: shell.webView)
                .ignoresSafeArea(edges: .bottom)
                .opacity(shell.state == .ready ? 1 : 0)
            switch shell.state {
            case .idle, .loading:
                ProgressView()
            case .unreachable:
                UnreachableView(retry: shell.refresh)
            case .ready:
                EmptyView()
            }
        }
        .background(Color(.systemBackground))
    }
}

struct UnreachableView: View {
    var message = WebShell.unreachableMessage
    var retry: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label(message, systemImage: "wifi.exclamationmark")
        } description: {
            Text("OpenCozy only answers on the home network. Connect to Wi-Fi at home or turn on the VPN.")
        } actions: {
            Button("Try again", action: retry).buttonStyle(.borderedProminent)
        }
    }
}

private struct WebViewHost: UIViewRepresentable {
    let webView: WKWebView
    func makeUIView(context: Context) -> WKWebView { webView }
    func updateUIView(_ uiView: WKWebView, context: Context) {}
}

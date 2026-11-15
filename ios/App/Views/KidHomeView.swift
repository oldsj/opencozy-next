import SwiftUI

/// The child's screen once set up: the web app's kid pages (Today, rewards,
/// screen time), signed in as this device's child. Native around it: the Parent
/// button and, when the server has rejected the device, why it's locked.
///
/// Shields re-sync when the page posts `screenTimeChanged` after a redemption,
/// whenever a page finishes loading, on pull to refresh, and on the app's own
/// 60-second and foreground syncs (see `RootView`).
struct KidHomeView: View {
    @Environment(AppModel.self) private var model
    var openParent: () -> Void
    @State private var shell: WebShell?
    @State private var session: KidWebSession?

    var body: some View {
        content
            .safeAreaInset(edge: .top, spacing: 0) { bar }
            // A new pairing (possibly another server or child) starts over.
            .task(id: model.pairingRevision) { await start() }
    }

    @ViewBuilder
    private var content: some View {
        switch session?.state {
        case nil, .idle, .signingIn:
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        case .signedIn:
            if let shell { WebShellView(shell: shell) }
        case .unreachable:
            UnreachableView { Task { await session?.signIn(userInitiated: true) } }
        case .failed(let message):
            ContentUnavailableView {
                Label("Can't open OpenCozy", systemImage: "exclamationmark.triangle")
            } description: {
                Text(model.accessProblem ?? message)
            } actions: {
                Button("Try again") { Task { await session?.signIn(userInitiated: true) } }
                    .buttonStyle(.borderedProminent)
            }
        }
    }

    private var bar: some View {
        HStack(spacing: 12) {
            if let problem = model.accessProblem {
                Label(problem, systemImage: "lock.trianglebadge.exclamationmark.fill")
                    .font(.footnote)
                    .foregroundStyle(.red)
                    .lineLimit(2)
            }
            Spacer(minLength: 0)
            Button("Parent", systemImage: "lock.shield", action: openParent)
                .labelStyle(.iconOnly)
                .font(.title3)
                .frame(minWidth: 44, minHeight: 44)
                .accessibilityLabel("Parent settings")
        }
        .padding(.horizontal, 12)
        .background(.bar)
    }

    private func start() async {
        guard let origin = model.config.baseURL, let deviceId = model.config.deviceId else { return }
        // Before the first exchange for this pairing: wipe any adult (or other
        // device's) cookies, which the server would otherwise prefer.
        await WebDataGate.shared.prepare(for: .kid(deviceId: deviceId, origin: origin))
        let shell = WebShell(origin: origin)
        let session = KidWebSession(exchange: { try await model.webSessionURL() }, load: { [weak shell] in shell?.load($0) })
        // Weak captures: the shell owns these closures.
        shell.onSignedOut = { [weak session] in Task { await session?.signIn() } }
        shell.onMessage = { message in
            switch message {
            case .screenTimeChanged: Task { await model.refresh() }
            }
        }
        shell.onPageLoaded = { Task { await model.refresh() } }
        shell.onRefresh = { [weak shell, weak session] in
            // A one-time exchange URL can't be repeated; get a new one.
            if shell?.state == .ready { shell?.reload() } else { Task { await session?.signIn(userInitiated: true) } }
            Task { await model.refresh() }
        }
        self.shell = shell
        self.session = session
        await session.signIn()
    }
}

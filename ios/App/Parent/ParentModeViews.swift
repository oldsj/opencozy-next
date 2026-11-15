import SwiftUI
import UIKit

/// Parent phone root: the OpenCozy web app (inbox, manage, everything), signed in
/// through the web as the adult. Native only for the server address and for lock-
/// screen notifications, which need this phone's own parent session.
struct ParentRootView: View {
    @Environment(ParentModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            if let origin = model.origin {
                ParentHomeView(origin: origin).id(origin)
            } else {
                NavigationStack { ParentConnectView() }
            }
        }
        // At launch too (the phase is already active then): picks up an adult's
        // revoke of this phone straight away, not at the next foreground.
        .task { if model.isSignedIn { await model.refresh() } }
        .onChange(of: scenePhase) { _, phase in
            // Keeps the badge and delivered notifications in step with the inbox.
            if phase == .active, model.isSignedIn { Task { await model.refresh() } }
        }
    }
}

/// First run on a parent phone: which OpenCozy server.
struct ParentConnectView: View {
    @Environment(ParentModel.self) private var model
    @Environment(RoleStore.self) private var roles
    @State private var server = ""
    @State private var error: String?

    var body: some View {
        Form {
            Section {
                TextField("https://opencozy.example.org", text: $server)
                    .textContentType(.URL).keyboardType(.URL)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                    .onSubmit(connect)
            } header: {
                Text("OpenCozy address")
            } footer: {
                Text(error ?? "Works at home or over the VPN.")
                    .foregroundStyle(error == nil ? Color.secondary : .red)
            }
            Section {
                Button("Continue", action: connect).disabled(server.isEmpty)
            }
            Section {
                Button("This is a child's device instead") { roles.role = nil }
            }
        }
        .navigationTitle("OpenCozy")
    }

    private func connect() {
        error = model.connect(server) ? nil : ParentModel.addressHint
    }
}

/// The web app, with a slim native bar for notifications and the server.
struct ParentHomeView: View {
    @Environment(ParentModel.self) private var model
    @Environment(RoleStore.self) private var roles
    let origin: URL
    @State private var shell: WebShell?
    @State private var showingNotifications = false

    var body: some View {
        Group {
            if let shell { WebShellView(shell: shell) } else { ProgressView() }
        }
        .safeAreaInset(edge: .top, spacing: 0) { bar }
        .sheet(isPresented: $showingNotifications) {
            NavigationStack { ParentNotificationsView() }
        }
        .task {
            guard shell == nil else { return }
            // Wipe web data a kid session (or another server) left behind first.
            await WebDataGate.shared.prepare(for: .parent(origin: origin))
            let made = WebShell(origin: origin)
            var watcher = WebSignInWatcher(origin: origin)
            // After the web sign-in, mint this phone's notification session from the
            // web cookie, natively: no second sign-in, and the token never enters
            // the page. The bell sheet remains the fallback.
            made.onURLChange = { [weak made, model, origin] url in
                guard watcher.shouldTry(after: url), !model.isSignedIn else { return }
                Task {
                    let result = await model.enableFromWebSession(deviceName: model.phoneName) { name in
                        guard let made, let cookie = WebCookies.parentHeader(await made.cookies(), url: origin.appending(path: "api/parent-sessions")) else {
                            return nil
                        }
                        return try await ParentSessionMint.send(ParentSessionMint.request(origin: origin, cookie: cookie, deviceName: name))
                    }
                    if result == .enabled { AppDelegate.registerForPush() }
                }
            }
            // The inbox is a parent phone's home; signed out, it offers sign-in.
            made.load(origin.appending(path: "manage/inbox"))
            shell = made
        }
    }

    private var bar: some View {
        HStack(spacing: 4) {
            Spacer(minLength: 0)
            Button {
                showingNotifications = true
            } label: {
                Image(systemName: model.isSignedIn ? "bell.badge" : "bell.slash")
                    .frame(minWidth: 44, minHeight: 44)
            }
            .accessibilityLabel(model.isSignedIn ? "Notifications on" : "Turn on notifications")
            Menu {
                Button("Reload", systemImage: "arrow.clockwise") { shell?.reload() }
                Button("Change server", systemImage: "server.rack") { Task { await model.disconnect() } }
                Button("This is a child's device", systemImage: "figure.child") {
                    Task {
                        await model.disconnect()
                        roles.role = nil
                    }
                }
            } label: {
                Image(systemName: "ellipsis.circle").frame(minWidth: 44, minHeight: 44)
            }
            .accessibilityLabel("More")
        }
        .font(.title3)
        .padding(.horizontal, 8)
        .background(.bar)
    }
}

/// Lock-screen Approve/Reject needs this phone's own parent session (the web
/// view's cookie can't register for push or act from a notification).
struct ParentNotificationsView: View {
    @Environment(ParentModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var email = ""
    @State private var password = ""
    @State private var deviceName = ""
    @State private var working = false

    private var server: String { model.origin?.absoluteString ?? "" }

    var body: some View {
        Form {
            if model.isSignedIn {
                signedIn
            } else {
                signIn
            }
            if let error = model.errorMessage {
                Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            }
        }
        .navigationTitle("Notifications")
        .onAppear { if deviceName.isEmpty { deviceName = model.phoneName } }
        .toolbar {
            ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
        }
    }

    @ViewBuilder
    private var signedIn: some View {
        Section {
            if let household = model.household { LabeledContent("Household", value: household.name) }
            if let session = model.session {
                LabeledContent("This phone", value: session.deviceName)
                LabeledContent("Session expires") { Text(session.expiresAt, format: .dateTime.day().month().year()) }
            }
            Button("Turn on notifications") { AppDelegate.registerForPush() }
        } footer: {
            Text("New chores and routines to approve arrive here. Approve or reject from the notification; Face ID or the passcode is needed first.")
        }
        Section {
            Button("Stop notifications", role: .destructive) { Task { await model.stopNotifications() } }
        } footer: {
            Text("Removes this phone's notification session. It doesn't sign you out of the web app, and signing in there again won't turn notifications back on; sign in here instead.")
        }
    }

    @ViewBuilder
    private var signIn: some View {
        Section {
            Text("Notifications turn on by themselves when you sign in to OpenCozy in the app. If they didn't, sign this phone in here.")
        }
        Section("Email and password") {
            TextField("Email", text: $email)
                .textContentType(.username).keyboardType(.emailAddress)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
            SecureField("Password", text: $password)
                .textContentType(.password)
            TextField("Phone name", text: $deviceName)
            Button {
                working = true
                Task {
                    model.phoneName = deviceName
                    if await model.signIn(server: server, email: email, password: password, deviceName: model.phoneName) {
                        password = ""
                        AppDelegate.registerForPush()
                    }
                    working = false
                }
            } label: {
                HStack { Text("Turn on notifications"); if working { Spacer(); ProgressView() } }
            }
            .disabled(working || email.isEmpty || password.isEmpty)
        }
        Section {
            Button {
                working = true
                Task {
                    let authenticator = PasskeyAuthenticator()
                    model.phoneName = deviceName
                    if await model.signInWithPasskey(server: server, deviceName: model.phoneName, authenticate: { rpId, challenge in
                        try await authenticator.assert(relyingParty: rpId, challenge: challenge)
                    }) {
                        AppDelegate.registerForPush()
                    }
                    working = false
                }
            } label: {
                Label("Use a passkey", systemImage: "person.badge.key")
            }
            .disabled(working)
        }
    }
}

import SwiftUI

@main
struct OpenCozyApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        WindowGroup {
            switch delegate.roles.role {
            case nil:
                RoleChoiceView(roles: delegate.roles)
            case .kid:
                RootView()
                    .environment(delegate.kid)
                    .environment(delegate.roles)
            case .parent:
                ParentRootView()
                    .environment(delegate.parent)
                    .environment(delegate.roles)
            }
        }
    }
}

/// First run: is this the child's device or a parent's phone?
struct RoleChoiceView: View {
    let roles: RoleStore

    var body: some View {
        VStack(spacing: 32) {
            VStack(spacing: 8) {
                Text("Welcome to OpenCozy").font(.largeTitle.bold())
                Text("How will this device be used?").foregroundStyle(.secondary)
            }
            VStack(spacing: 16) {
                Button { roles.role = .kid } label: {
                    RoleCard(title: "A child's device", detail: "Chores, routines and screen time for one child. Locks chosen apps until time is earned.",
                             systemImage: "figure.child")
                }
                Button { roles.role = .parent } label: {
                    RoleCard(title: "A parent's device", detail: "The whole of OpenCozy, with notifications to approve chores and routines. Doesn't lock anything.",
                             systemImage: "person.badge.shield.checkmark")
                }
            }
            .buttonStyle(.plain)
        }
        .padding(24)
        .frame(maxWidth: 560)
    }
}

private struct RoleCard: View {
    let title: String
    let detail: String
    let systemImage: String

    var body: some View {
        HStack(spacing: 16) {
            Image(systemName: systemImage).font(.largeTitle).frame(width: 56)
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.title3.bold())
                Text(detail).font(.callout).foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(20)
        .background(.fill.tertiary, in: .rect(cornerRadius: 16))
        .contentShape(.rect)
    }
}

struct RootView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var gate = ParentGate()
    @State private var wasInBackground = false

    var body: some View {
        Group {
            switch model.phase {
            case .needsAuthorization:
                KidStack(openParent: gate.open, parentDisabled: true) { AuthorizationView() }
            case .needsSetup:
                KidStack(openParent: gate.open) { NeedsSetupView(openParent: gate.open) }
            case .ready:
                KidHomeView(openParent: gate.open)
            }
        }
        .sheet(isPresented: $gate.isPresented) {
            ParentGateView().environment(gate)
        }
        .task {
            // Launch sync, then a light periodic sync while the app is on screen so a
            // reward redeemed elsewhere (for example the wall display) unlocks here
            // without the page telling us. Suspended in background.
            await model.refresh()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                if scenePhase == .active { await model.refresh() }
            }
        }
        .onChange(of: scenePhase) { _, phase in
            gate.scenePhaseChanged(phase)
            // Only a real return from the background. System prompts (camera,
            // Screen Time) just bounce through inactive, and a refresh then would
            // re-sample authorization mid-flow for nothing.
            if phase == .background { wasInBackground = true }
            if phase == .active, wasInBackground {
                wasInBackground = false
                model.refreshAuthorizationStatus()
                Task { await model.refresh() }
            }
        }
    }
}

/// A navigation stack with the PIN-protected Parent button.
struct KidStack<Content: View>: View {
    var openParent: () -> Void
    var parentDisabled = false
    @ViewBuilder var content: Content

    var body: some View {
        NavigationStack {
            content
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Parent", systemImage: "lock.shield") { openParent() }
                            .disabled(parentDisabled)
                    }
                }
        }
    }
}

struct NeedsSetupView: View {
    var openParent: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label("Almost ready", systemImage: "gearshape.2")
        } description: {
            Text("A parent needs to connect OpenCozy and choose which apps stay locked.")
        } actions: {
            Button("Parent setup", action: openParent)
                .buttonStyle(.borderedProminent)
        }
    }
}

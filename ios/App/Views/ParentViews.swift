import FamilyControls
import SwiftUI

/// Whether the parent area is open and unlocked. Lives above the sheet (in
/// `RootView`), not in the sheet's own `@State`, so a sheet that gets rebuilt or
/// re-presented doesn't throw away the unlock. It locks when the parent taps Done,
/// when the app goes to the background (the camera or permission prompts only make
/// it inactive), and on reopening more than `reopenGrace` after unlocking.
@MainActor
@Observable
final class ParentGate {
    static let reopenGrace: TimeInterval = 5 * 60

    var isPresented = false
    private(set) var unlockedAt: Date?
    @ObservationIgnored private let now: () -> Date

    init(now: @escaping () -> Date = Date.init) { self.now = now }

    var isUnlocked: Bool { unlockedAt != nil }

    func open() {
        if let unlockedAt, now().timeIntervalSince(unlockedAt) > Self.reopenGrace { lock() }
        isPresented = true
    }

    func unlock() { unlockedAt = now() }

    /// Done: close and lock.
    func close() {
        lock()
        isPresented = false
    }

    func lock() { unlockedAt = nil }

    func scenePhaseChanged(_ phase: ScenePhase) {
        if phase == .background { lock() }
    }
}

/// Presents PIN creation on first use, then PIN entry, then the setup screen.
struct ParentGateView: View {
    @Environment(ParentGate.self) private var gate
    @State private var pinIsSet = ParentPIN().isSet

    var body: some View {
        NavigationStack {
            Group {
                if gate.isUnlocked {
                    ParentSetupView()
                } else if pinIsSet {
                    PINEntryView { gate.unlock() }
                } else {
                    PINCreateView {
                        pinIsSet = true
                        gate.unlock()
                    }
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { gate.close() }
                }
            }
        }
        // Only Done closes it (and locks); no swipe or tap-outside dismissal.
        .interactiveDismissDisabled()
    }
}

struct PINEntryView: View {
    var onSuccess: () -> Void
    @State private var pin = ""
    @State private var wrong = false
    @State private var lockedUntil: Date? = ParentPIN().throttle.lockedUntil

    var body: some View {
        Form {
            Section {
                SecureField("Parent PIN", text: $pin)
                    .keyboardType(.numberPad)
                    .textContentType(.password)
                    .onSubmit(check)
                Button("Unlock", action: check)
                    .disabled(pin.isEmpty)
            } footer: {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    if let lockedUntil, lockedUntil > context.date {
                        Text("Too many attempts. Try again \(lockedUntil, format: .relative(presentation: .named)).")
                    } else if wrong {
                        Text("Wrong PIN.").foregroundStyle(.red)
                    }
                }
            }
        }
        .navigationTitle("Parent")
    }

    private func check() {
        switch ParentPIN().attempt(pin) {
        case .success:
            onSuccess()
        case .wrong(let until):
            wrong = true
            lockedUntil = until
        case .throttled(let until):
            lockedUntil = until
        }
        pin = ""
    }
}

struct PINCreateView: View {
    var onCreated: () -> Void
    @State private var pin = ""
    @State private var confirm = ""
    @State private var error: String?

    var body: some View {
        Form {
            Section {
                SecureField("New PIN (4–8 digits)", text: $pin).keyboardType(.numberPad)
                SecureField("Confirm PIN", text: $confirm).keyboardType(.numberPad)
            } header: {
                Text("Create a parent PIN")
            } footer: {
                Text(error ?? "The PIN keeps OpenCozy's settings away from kids. It's stored only on this device.")
                    .foregroundStyle(error == nil ? Color.secondary : .red)
            }
            Button("Save PIN") {
                guard ParentPIN.isValid(pin) else { error = "Use 4 to 8 digits."; return }
                guard pin == confirm else { error = "The PINs don't match."; return }
                if ParentPIN().set(pin) { onCreated() } else { error = "Couldn't save the PIN." }
            }
        }
        .navigationTitle("Parent")
    }
}

struct ParentSetupView: View {
    @Environment(AppModel.self) private var model
    @State private var selection = FamilyActivitySelection(includeEntireCategory: true)
    @State private var showingPicker = false
    @State private var allowed = FamilyActivitySelection()
    @State private var showingAllowedPicker = false
    @State private var newPIN = ""
    @State private var selectionError: String?
    @State private var selfTestResult: String?
    @State private var selfTesting = false
    @State private var pairing = PairingFlow()

    var body: some View {
        Form {
            PairingSection(flow: pairing)
            selectionSection
            statusSection
            pinSection
        }
        .navigationTitle("Parent setup")
        .pairingPresentation(pairing, model: model)
        .onAppear {
            selection = model.lock.selection ?? FamilyActivitySelection(includeEntireCategory: true)
            allowed = model.lock.alwaysAllowedSelection
        }
        .familyActivityPicker(isPresented: $showingPicker, selection: $selection)
        .onChange(of: showingPicker) { _, showing in
            if !showing { Task { selectionError = await model.saveSelection(selection) } }
        }
        .familyActivityPicker(headerText: "Pick Messages, FaceTime, Phone and Contacts", isPresented: $showingAllowedPicker, selection: $allowed)
        .onChange(of: showingAllowedPicker) { _, showing in
            if !showing { Task { selectionError = await model.saveAlwaysAllowed(allowed) } }
        }
    }

    private var selectionSection: some View {
        Section {
            Button("Choose apps to lock…") { showingPicker = true }
            LabeledContent("Apps", value: "\(selection.applicationTokens.count)")
            LabeledContent("Categories", value: "\(selection.categoryTokens.count)")
            Button("Always allowed…") { showingAllowedPicker = true }
            ForEach(Array(allowed.applicationTokens), id: \.self) { token in
                Label(token).labelStyle(.titleAndIcon)
            }
        } header: {
            Text("Locked apps")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if let selectionError { Text(selectionError).foregroundStyle(.red) }
                Text(CommunicationApps.pickerNote)
                Text("To make sure, add Messages, FaceTime, Phone and Contacts under Always allowed: they're never locked, even inside a locked category.")
                Text("These apps stay locked except while screen time is running. Don't include OpenCozy itself, or a category that contains it.")
                Text("Websites are never locked here, so browsers stay open for school and Wikipedia. Add Safari and Chrome under Always allowed. Websites picked here are ignored.")
            }
        }
    }

    /// The household's blocked websites from the last sync (Manage → Devices).
    private var webFilterDescription: String {
        guard let lists = model.lock.store.webFilterLists else { return "Not synced yet" }
        let all = WebFilterPlan.plan(lists, windowOpen: false)
        if all.domains.isEmpty { return "None" }
        let summary = "\(lists.gamePortals.count) always, \(lists.games.count) games outside screen time"
        return all.dropped > 0 ? summary + " (\(all.dropped) over Apple's limit of \(WebFilterPlan.maxDomains) not blocked)" : summary
    }

    private var statusSection: some View {
        Section("Status") {
            if let problem = model.sharedStorageProblem {
                Label(problem, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red)
            }
            LabeledContent("Screen Time permission", value: model.authorizationStatus == .approved ? "Approved" : "Not approved")
            if model.authorizationStatus != .approved {
                Button("Request permission") { Task { await model.requestAuthorization() } }
            }
            LabeledContent("Shield", value: shieldDescription)
            LabeledContent("Blocked websites", value: webFilterDescription)
            if let lastSync = model.lastSync {
                LabeledContent("Last sync", value: lastSync.formatted(date: .omitted, time: .standard))
            }
            Button("Sync now") { Task { await model.refresh() } }
            Button {
                selfTesting = true
                Task {
                    selfTestResult = await model.runSelfTest()
                    selfTesting = false
                }
            } label: {
                if selfTesting { ProgressView() } else { Text("Test the lock extension") }
            }
            .disabled(selfTesting || model.authorizationStatus != .approved)
            if let selfTestResult { Text(selfTestResult).font(.footnote) }
        }
    }

    private var pinSection: some View {
        Section("Parent PIN") {
            SecureField("New PIN (4–8 digits)", text: $newPIN).keyboardType(.numberPad)
            Button("Change PIN") {
                if ParentPIN().set(newPIN) { newPIN = "" }
            }
            .disabled(!ParentPIN.isValid(newPIN))
        }
    }

    private var shieldDescription: String {
        switch model.lockOutcome {
        case .shielded: "Locked"
        case .unlocked(let until): "Unlocked until \(until.formatted(date: .omitted, time: .shortened))"
        case .noSelection: "No apps chosen"
        case nil: "Unknown"
        }
    }
}

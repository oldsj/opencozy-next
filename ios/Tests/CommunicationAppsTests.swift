import FamilyControls
import Foundation
import Testing
@testable import OpenCozy

struct CommunicationAppsTests {
    @Test func recognisesMessagesFaceTimePhoneAndContacts() {
        for id in ["com.apple.MobileSMS", "com.apple.facetime", "com.apple.mobilephone", "com.apple.MobileAddressBook"] {
            #expect(CommunicationApps.isCommunication(bundleIdentifier: id), "\(id)")
        }
        #expect(CommunicationApps.isCommunication(bundleIdentifier: "COM.APPLE.MOBILESMS"))
        #expect(!CommunicationApps.isCommunication(bundleIdentifier: "com.example.game"))
        #expect(!CommunicationApps.isCommunication(bundleIdentifier: "com.apple.mobilesafari"))
        #expect(!CommunicationApps.isCommunication(bundleIdentifier: nil))
    }

    @Test func pickerNoteSaysWhereToLimitContacts() {
        #expect(CommunicationApps.pickerNote == "Messages and FaceTime are always allowed; set who they can contact in Screen Time → Communication Limits.")
    }
}

/// Strings stand in for the opaque tokens, which only the picker can create.
struct ShieldPlanTests {
    typealias Plan = ShieldPlan<String, String>

    @Test func pickedCommunicationAppsAreStripped() {
        let plan = Plan.make(applications: ["game", "messages", "facetime"], categories: [], alwaysAllowed: ["messages", "facetime", "phone"])
        #expect(plan.applications == ["game"])
        #expect(plan.categories == nil)
        #expect(plan.categoryExceptions.isEmpty)
    }

    @Test func lockedCategoriesExemptThem() {
        // "Social" contains Messages and FaceTime; the exception keeps them open.
        let plan = Plan.make(applications: [], categories: ["social", "games"], alwaysAllowed: ["messages", "facetime"])
        #expect(plan.applications == nil)
        #expect(plan.categories == ["social", "games"])
        #expect(plan.categoryExceptions == ["messages", "facetime"])
    }

    @Test func onlyCommunicationPickedLocksNothing() {
        let plan = Plan.make(applications: ["messages"], categories: [], alwaysAllowed: ["messages"])
        #expect(plan.applications == nil)
        #expect(plan.categories == nil)
    }

    @Test func nothingAllowedLeavesTheSelectionAlone() {
        let plan = Plan.make(applications: ["game"], categories: ["games"], alwaysAllowed: [])
        #expect(plan.applications == ["game"])
        #expect(plan.categories == ["games"])
        #expect(plan.categoryExceptions.isEmpty)
    }
}

struct AlwaysAllowedStorageTests {
    @Test func emptyByDefaultAndRoundTrips() throws {
        let suite = "org.opencozy.tests.\(UUID().uuidString)"
        let lock = ScreenTimeLock(store: SharedStore(suiteName: suite))
        #expect(lock.alwaysAllowed.isEmpty)
        #expect(lock.alwaysAllowedSelection.applicationTokens.isEmpty)
        try lock.saveAlwaysAllowed(FamilyActivitySelection())
        #expect(SharedStore(suiteName: suite).alwaysAllowedData != nil)
        #expect(lock.alwaysAllowed.isEmpty)
    }

    @Test func refusedWithoutTheAppGroup() {
        let lock = ScreenTimeLock(store: SharedStore(suiteName: nil))
        #expect(throws: ScreenTimeLock.SelectionError.sharedStorageUnavailable) { try lock.saveAlwaysAllowed(FamilyActivitySelection()) }
    }

    @Test func unreadableDataCountsAsNone() {
        let suite = "org.opencozy.tests.\(UUID().uuidString)"
        let store = SharedStore(suiteName: suite)
        store.alwaysAllowedData = Data("not json".utf8)
        store.detectedCommunicationData = Data("[]".utf8)
        #expect(ScreenTimeLock(store: store).alwaysAllowed.isEmpty)
    }
}

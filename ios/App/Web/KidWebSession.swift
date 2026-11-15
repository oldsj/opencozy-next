import Foundation
import Observation

/// Signs the kid device's web view in as its child: a signed
/// `POST /api/device/web-session` returns a one-time exchange URL, and opening
/// it sets the child-device session cookie. Done on launch and whenever the web
/// view reports the session ended (sign-in page or 401). Exchanges are capped so a
/// server that keeps refusing the cookie can't make the app loop.
@MainActor
@Observable
final class KidWebSession {
    enum State: Equatable {
        case idle
        case signingIn
        case signedIn
        case unreachable
        case failed(String)
    }

    static let maxExchanges = 3
    static let exchangeWindow: TimeInterval = 2 * 60
    static let loopMessage = "OpenCozy keeps ending this device's session. A parent can check it in OpenCozy under Manage → Devices, then try again."
    static let unsupportedMessage = "This OpenCozy server doesn't support the app's web view yet. Update OpenCozy on the server, then try again."

    private(set) var state: State = .idle
    @ObservationIgnored private var recent: [Date] = []
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private let exchange: @MainActor () async throws -> URL
    @ObservationIgnored private let load: @MainActor (URL) -> Void

    init(now: @escaping () -> Date = Date.init,
         exchange: @escaping @MainActor () async throws -> URL,
         load: @escaping @MainActor (URL) -> Void) {
        self.now = now
        self.exchange = exchange
        self.load = load
    }

    /// Gets a fresh exchange URL and loads it. `userInitiated` (Try again) resets
    /// the loop guard.
    func signIn(userInitiated: Bool = false) async {
        guard state != .signingIn else { return }
        let time = now()
        if userInitiated { recent = [] }
        recent = recent.filter { time.timeIntervalSince($0) < Self.exchangeWindow }
        guard recent.count < Self.maxExchanges else {
            state = .failed(Self.loopMessage)
            return
        }
        recent.append(time)
        state = .signingIn
        do {
            load(try await exchange())
            state = .signedIn
        } catch APIError.transport {
            state = .unreachable
        } catch APIError.server(404, _) {
            state = .failed(Self.unsupportedMessage)
        } catch APIError.server(let status, _) where status >= 500 {
            state = .failed("OpenCozy had a problem. Try again in a moment.")
        } catch {
            state = .failed(error.localizedDescription)
        }
    }
}

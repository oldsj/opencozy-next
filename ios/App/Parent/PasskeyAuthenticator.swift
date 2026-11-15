import AuthenticationServices
import UIKit

/// The pieces of a WebAuthn assertion the server verifies, base64url-encoded as
/// WebAuthn JSON expects.
struct PasskeyAssertion: Equatable, Sendable {
    var credentialId: String
    var clientDataJSON: String
    var authenticatorData: String
    var signature: String
    var userHandle: String?
}

/// Runs a passkey sign-in with the system sheet. The relying party is the canonical
/// OpenCozy host (from the server, never hard-coded); the app needs the matching
/// `webcredentials:` Associated Domain and the server's apple-app-site-association.
@MainActor
final class PasskeyAuthenticator: NSObject, ASAuthorizationControllerDelegate, ASAuthorizationControllerPresentationContextProviding {
    enum PasskeyError: LocalizedError {
        case cancelled
        case unexpectedCredential
        case failed(String)

        var errorDescription: String? {
            switch self {
            case .cancelled: "Passkey sign-in was cancelled."
            case .unexpectedCredential: "The passkey sheet returned something unexpected."
            case .failed(let message): message
            }
        }
    }

    private var continuation: CheckedContinuation<PasskeyAssertion, Error>?

    /// `challenge` is the raw bytes from the server's assertion options.
    /// `allowedCredentialIds` (raw bytes) narrows the choice; empty lets the user pick.
    func assert(relyingParty: String, challenge: Data, allowedCredentialIds: [Data] = []) async throws -> PasskeyAssertion {
        let provider = ASAuthorizationPlatformPublicKeyCredentialProvider(relyingPartyIdentifier: relyingParty)
        let request = provider.createCredentialAssertionRequest(challenge: challenge)
        if !allowedCredentialIds.isEmpty {
            request.allowedCredentials = allowedCredentialIds.map(ASAuthorizationPlatformPublicKeyCredentialDescriptor.init(credentialID:))
        }
        let controller = ASAuthorizationController(authorizationRequests: [request])
        controller.delegate = self
        controller.presentationContextProvider = self
        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            controller.performRequests()
        }
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithAuthorization authorization: ASAuthorization) {
        guard let credential = authorization.credential as? ASAuthorizationPlatformPublicKeyCredentialAssertion else {
            finish(.failure(PasskeyError.unexpectedCredential))
            return
        }
        finish(.success(PasskeyAssertion(
            credentialId: credential.credentialID.base64URL,
            clientDataJSON: credential.rawClientDataJSON.base64URL,
            authenticatorData: credential.rawAuthenticatorData.base64URL,
            signature: credential.signature.base64URL,
            userHandle: credential.userID.isEmpty ? nil : credential.userID.base64URL)))
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: Error) {
        if let error = error as? ASAuthorizationError, error.code == .canceled {
            finish(.failure(PasskeyError.cancelled))
        } else {
            finish(.failure(PasskeyError.failed(error.localizedDescription)))
        }
    }

    func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        if let key = scenes.flatMap(\.windows).first(where: \.isKeyWindow) { return key }
        // Sign-in is only reachable from a visible scene, so one exists.
        let scene = scenes.first(where: { $0.activationState == .foregroundActive }) ?? scenes[0]
        return ASPresentationAnchor(windowScene: scene)
    }

    private func finish(_ result: Result<PasskeyAssertion, Error>) {
        continuation?.resume(with: result)
        continuation = nil
    }
}

extension Data {
    /// base64url without padding (RFC 4648 §5), as WebAuthn uses.
    var base64URL: String {
        base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    init?(base64URL: String) {
        var s = base64URL.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        s += String(repeating: "=", count: (4 - s.count % 4) % 4)
        self.init(base64Encoded: s)
    }
}

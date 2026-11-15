import Foundation
import Testing
@testable import OpenCozy

/// `HTTPParentAPI` against the inbox contract (api/src/inbox.yaml): the path, the
/// phone bearer token, the Idempotency-Key header and the body.
@Suite(.serialized)
struct InboxWireTests {
    static let id = "0b9e2f4a-6c1d-4e8b-9a37-5f2d8c1e6b40"

    private func api() -> HTTPParentAPI {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [InboxCapturingProtocol.self]
        return HTTPParentAPI(baseURL: URL(string: "https://opencozy.example.org")!, token: "ocp_test", session: URLSession(configuration: config))
    }

    @Test func approvePostsAnEmptyObjectWithTheKey() async throws {
        let decision = try await api().approve(id: Self.id, idempotencyKey: "\(Self.id):approve")
        #expect(decision == Decision(id: Self.id, state: .approved))
        let request = try #require(InboxCapturingProtocol.last)
        #expect(request.httpMethod == "POST")
        #expect(request.url?.path == "/api/inbox/\(Self.id)/approve")
        #expect(request.value(forHTTPHeaderField: "Idempotency-Key") == "\(Self.id):approve")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer ocp_test")
        let body = try JSONSerialization.jsonObject(with: try #require(InboxCapturingProtocol.lastBody)) as? [String: Any]
        #expect(body?.isEmpty == true)
    }

    @Test func rejectSendsItsKey() async throws {
        _ = try await api().reject(id: Self.id, RejectRequest(reason: nil), idempotencyKey: "\(Self.id):reject")
        let request = try #require(InboxCapturingProtocol.last)
        #expect(request.url?.path == "/api/inbox/\(Self.id)/reject")
        #expect(request.value(forHTTPHeaderField: "Idempotency-Key") == "\(Self.id):reject")
    }

    @Test func inboxIsAPlainGet() async throws {
        let result = try await api().inbox()
        #expect(result.items.isEmpty)
        let request = try #require(InboxCapturingProtocol.last)
        #expect(request.httpMethod == "GET")
        #expect(request.url?.path == "/api/inbox")
        #expect(request.value(forHTTPHeaderField: "Idempotency-Key") == nil)
    }
}

final class InboxCapturingProtocol: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var last: URLRequest?
    nonisolated(unsafe) static var lastBody: Data?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.last = request
        Self.lastBody = request.httpBody ?? request.httpBodyStream.map(Self.read)
        let path = request.url?.path ?? ""
        let json: String
        if path.hasSuffix("/approve") {
            json = #"{"id":"\#(InboxWireTests.id)","state":"approved"}"#
        } else if path.hasSuffix("/reject") {
            json = #"{"id":"\#(InboxWireTests.id)","state":"rejected"}"#
        } else {
            json = #"{"items":[]}"#
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(json.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static func read(_ stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data
    }
}

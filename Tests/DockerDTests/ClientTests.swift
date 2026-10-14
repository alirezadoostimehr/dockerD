import XCTest

@testable import DockerD

final class ClientTests: XCTestCase {
  func testChunkedResponseAndExtensions() throws {
    let response = Data(
      "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4;part=1\r\n{\"a\"\r\n3\r\n:1}\r\n0\r\n\r\n"
        .utf8)
    XCTAssertEqual(
      String(data: try ServiceClient.decodeHTTP(response), encoding: .utf8), "{\"a\":1}")
  }
  func testTruncatedChunkIsRejected() {
    XCTAssertThrowsError(
      try ServiceClient.decodeHTTP(
        Data("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n10\r\nshort".utf8)))
  }
  func testServiceErrorIsPreserved() {
    let response = Data(
      "HTTP/1.1 400 Bad Request\r\nContent-Length: 29\r\n\r\n{\"error\":\"Stop engine first\"}".utf8
    )
    XCTAssertThrowsError(try ServiceClient.decodeHTTP(response)) {
      XCTAssertEqual($0.localizedDescription, "Stop engine first")
    }
  }
}

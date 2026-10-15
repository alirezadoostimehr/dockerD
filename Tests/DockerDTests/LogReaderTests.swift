import XCTest

@testable import DockerD

final class LogReaderTests: XCTestCase {
  func testMissingLogAndFileReplacement() throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let url = directory.appendingPathComponent("service.log")
    XCTAssertEqual(try LogReader.read(url).text, "")
    try Data("first startup\n".utf8).write(to: url)
    XCTAssertEqual(try LogReader.read(url).text, "first startup\n")
    try Data("new startup\n".utf8).write(to: url, options: .atomic)
    XCTAssertEqual(try LogReader.read(url).text, "new startup\n")
  }

  func testLargeLogReturnsOnlyRecentCompleteLines() throws {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: url) }
    let line = "engine: starting 🐳\n"
    let text = String(repeating: line, count: 10000) + "engine: ready\n"
    try Data(text.utf8).write(to: url)
    let result = try LogReader.read(url)
    XCTAssertTrue(result.truncated)
    XCTAssertLessThanOrEqual(result.text.utf8.count, LogReader.limit)
    XCTAssertTrue(result.text.hasPrefix(line))
    XCTAssertTrue(result.text.hasSuffix("engine: ready\n"))
    XCTAssertFalse(result.text.contains("�"))
  }
}

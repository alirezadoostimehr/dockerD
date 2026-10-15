import XCTest

@testable import DockerD

final class CountdownTests: XCTestCase {
  func testDeadlineCrossingNeverCountsUp() {
    let deadline = Date(timeIntervalSince1970: 1000)
    let offsets: [TimeInterval] = [-2, -1, -0.1, 0, 1, 2, 30]
    let values = offsets.map {
      CountdownView.remainingTime(until: deadline, now: deadline.addingTimeInterval($0))
    }
    XCTAssertEqual(values, ["00:02", "00:01", "00:01", "00:00", "00:00", "00:00", "00:00"])
  }

  func testNewDeadlineAndLongCountdown() {
    let now = Date(timeIntervalSince1970: 1000)
    XCTAssertEqual(
      CountdownView.remainingTime(until: now.addingTimeInterval(-5), now: now), "00:00")
    XCTAssertEqual(
      CountdownView.remainingTime(until: now.addingTimeInterval(65), now: now), "01:05")
    XCTAssertEqual(
      CountdownView.remainingTime(until: now.addingTimeInterval(3661), now: now), "01:01:01")
  }
}

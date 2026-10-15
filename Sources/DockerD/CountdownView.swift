import SwiftUI

struct CountdownView: View {
  let deadline: Date

  var body: some View {
    TimelineView(.periodic(from: .now, by: 1)) { context in
      Text(Self.remainingTime(until: deadline, now: context.date)).monospacedDigit()
    }
  }

  // A date styled as .timer counts upward once its date has passed. Keep the
  // countdown at zero while waiting for the next engine-status update instead.
  static func remainingTime(until deadline: Date, now: Date) -> String {
    let seconds = Int(max(0, deadline.timeIntervalSince(now)).rounded(.up))
    if seconds >= 3600 {
      return String(format: "%02d:%02d:%02d", seconds / 3600, seconds / 60 % 60, seconds % 60)
    }
    return String(format: "%02d:%02d", seconds / 60, seconds % 60)
  }
}

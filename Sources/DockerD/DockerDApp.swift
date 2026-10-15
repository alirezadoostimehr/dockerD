import AppKit
import SwiftUI

@main
struct DockerDApp: App {
  @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
  @StateObject private var model = AppModel()
  @AppStorage("appearance") private var appearance = "system"

  var body: some Scene {
    WindowGroup("DockerD", id: "main") {
      ContentView().environmentObject(model)
        .preferredColorScheme(appearance == "system" ? nil : appearance == "dark" ? .dark : .light)
        .onAppear { delegate.model = model }
    }
    .defaultSize(width: 1200, height: 800)
    .windowStyle(.hiddenTitleBar)
    .commands {
      CommandGroup(replacing: .appSettings) {
        Button("Settings…") { model.section = .settings }.keyboardShortcut(",")
      }
      CommandGroup(after: .toolbar) {
        Button("Refresh") { Task { await model.refresh() } }.keyboardShortcut("r")
        Button("Show Logs") { model.section = .logs }.keyboardShortcut(
          "l", modifiers: [.command, .shift])
      }
    }
    MenuBarExtra("DockerD", systemImage: "shippingbox.fill") {
      MenuBarContent().environmentObject(model)
    }.menuBarExtraStyle(.window)
  }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
  weak var model: AppModel?
  var canQuit = false
  func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
  func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
    guard !canQuit, let model, model.status?.running == true || model.status?.changing == true
    else { return .terminateNow }
    let alert = NSAlert()
    alert.messageText = "Quit DockerD?"
    alert.informativeText =
      "You can leave your engine running with idle monitoring, or stop it before quitting. Your data will be preserved."
    alert.addButton(withTitle: "Leave engine running")
    alert.addButton(withTitle: "Stop engine and quit")
    alert.addButton(withTitle: "Cancel")
    switch alert.runModal() {
    case .alertFirstButtonReturn: return .terminateNow
    case .alertSecondButtonReturn:
      Task {
        if model.status?.state == "starting" {
          model.error = "Wait for startup to finish before stopping the engine."
          return
        }
        await model.engine("stop")
        for _ in 0..<150 {
          await model.refresh()
          if model.status?.state == "stopped" {
            self.canQuit = true
            NSApp.terminate(nil)
            return
          }
          if model.status?.state == "error" {
            model.error = model.status?.error
            return
          }
          try? await Task.sleep(for: .seconds(2))
        }
        model.error = "Shutdown is taking longer than expected. Check diagnostics before quitting."
      }
      return .terminateCancel
    default: return .terminateCancel
    }
  }
}

struct MenuBarContent: View {
  @EnvironmentObject private var model: AppModel
  @Environment(\.openWindow) private var openWindow
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        BrandMark(size: 30)
        Text("DockerD").font(.headline)
        Spacer()
        StatusBadge(state: model.status?.state ?? "connecting")
      }
      Text(model.status?.message ?? "Connecting to your local engine…").font(.caption)
        .foregroundStyle(.secondary)
      if let status = model.status, status.running {
        Text(
          "\(status.activity.blockers.count) activity blocker\(status.activity.blockers.count == 1 ? "" : "s")"
        ).font(.callout)
        if let deadline = status.activity.deadlineDate {
          HStack {
            Text("Stops in")
            CountdownView(deadline: deadline)
          }.font(.caption)
        }
        Button("Keep running") { Task { await model.resetIdle() } }
        Button("Stop engine") { Task { await model.engine("stop") } }
      } else {
        Button("Start engine") { Task { await model.engine("start") } }.disabled(
          model.status?.changing == true)
      }
      Divider()
      Button("Open DockerD") {
        openWindow(id: "main")
        NSApp.activate(ignoringOtherApps: true)
      }
      Button("Quit DockerD…") { NSApp.terminate(nil) }
    }.padding(20).frame(width: 320)
  }
}

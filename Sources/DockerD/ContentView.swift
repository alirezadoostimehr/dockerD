import SwiftUI

let accent = Color(red: 0.19, green: 0.43, blue: 0.94)

struct BrandMark: View {
  var size: CGFloat = 40
  var body: some View {
    Image(systemName: "shippingbox.fill").font(.system(size: size * 0.50, weight: .semibold))
      .foregroundStyle(.white).frame(width: size, height: size)
      .background(
        LinearGradient(
          colors: [accent, Color(red: 0.30, green: 0.61, blue: 1)], startPoint: .bottomLeading,
          endPoint: .topTrailing), in: RoundedRectangle(cornerRadius: size * 0.26)
      )
      .accessibilityHidden(true)
  }
}
struct StatusBadge: View {
  let state: String
  var color: Color {
    switch state {
    case "active", "running", "Running", "listening": .green
    case "error", "Failed": .red
    case "idle countdown", "starting", "stopping", "Pending", "connecting": .orange
    default: .secondary
    }
  }
  var body: some View {
    HStack(spacing: 6) {
      Circle().fill(color).frame(width: 6, height: 6)
      Text(state == "active" ? "Running" : state.capitalized).lineLimit(1)
    }
    .font(.system(size: 11, weight: .medium)).padding(.horizontal, 9).padding(.vertical, 5)
    .background(color.opacity(0.10), in: Capsule())
  }
}
struct Surface<Content: View>: View {
  @ViewBuilder var content: Content
  var body: some View {
    content.padding(22).frame(maxWidth: .infinity, alignment: .leading).background(
      Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 16)
    ).overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(.primary.opacity(0.06)))
  }
}

struct ContentView: View {
  @EnvironmentObject private var model: AppModel
  @State private var confirmStop = false
  var body: some View {
    NavigationSplitView {
      VStack(alignment: .leading, spacing: 0) {
        HStack(spacing: 10) {
          BrandMark(size: 34)
          VStack(alignment: .leading, spacing: 1) {
            Text("DockerD").font(.system(size: 19, weight: .bold))
            Text("LOCAL WORKSPACE").font(.system(size: 8, weight: .semibold)).tracking(1.7)
              .foregroundStyle(.secondary)
          }
        }.padding(.horizontal, 20).padding(.top, 32).padding(.bottom, 28)
        Text("WORKSPACE").font(.system(size: 10, weight: .semibold)).tracking(1.3).foregroundStyle(
          .tertiary
        ).padding(.horizontal, 24).padding(.bottom, 8)
        ForEach(Section.allCases.filter { $0 != .settings }) { section in sidebarButton(section) }
        Spacer()
        sidebarButton(.settings)
        Divider().padding(.horizontal, 20).padding(.vertical, 16)
        HStack(spacing: 8) {
          Image(systemName: "desktopcomputer").foregroundStyle(.secondary)
          VStack(alignment: .leading, spacing: 3) {
            Text("Local engine").font(.system(size: 11, weight: .medium))
            Text("Apple Silicon · Lima").font(.system(size: 10)).foregroundStyle(.tertiary)
          }
          Spacer()
        }.padding(.horizontal, 24).padding(.bottom, 24)
      }.navigationSplitViewColumnWidth(min: 210, ideal: 220, max: 250)
    } detail: {
      VStack(spacing: 0) {
        HStack {
          Label("Personal workspace", systemImage: "square.stack.3d.up").font(.system(size: 11))
            .foregroundStyle(.secondary)
          Spacer()
          StatusBadge(state: model.connected ? model.status?.state ?? "connecting" : "disconnected")
          Button {
            model.section = .settings
          } label: {
            Image(systemName: "gearshape").foregroundStyle(.secondary)
          }.buttonStyle(.plain).help("Settings")
        }.padding(.horizontal, 30).frame(height: 52)
        Divider().opacity(0.5)
        HStack(alignment: .top) {
          VStack(alignment: .leading, spacing: 7) {
            Text(model.section.rawValue).font(.system(size: 29, weight: .bold))
            Text(model.section.subtitle).font(.system(size: 13)).foregroundStyle(.secondary)
          }
          Spacer()
          if model.section != .settings {
            if model.status?.running == true {
              Button {
                confirmStop = true
              } label: {
                Label("Stop engine", systemImage: "stop.fill")
              }.controlSize(.large)
            } else {
              Button {
                Task { await model.engine("start") }
              } label: {
                Label(
                  model.status?.changing == true ? "Please wait…" : "Start engine",
                  systemImage: "play.fill")
              }.buttonStyle(.borderedProminent).tint(accent).controlSize(.large).disabled(
                !model.connected || model.status?.changing == true)
            }
          }
        }.padding(30)
        if !model.connected {
          EmptyPanel(
            symbol: "cable.connector.slash", title: "Connecting to DockerD",
            description: "The local service manages your engine and keeps idle monitoring active."
          ) { Button("Reconnect") { Task { await model.connect() } } }
        } else {
          switch model.section {
          case .overview: OverviewView()
          case .settings: SettingsView()
          case .forwards: ForwardsView()
          default: ResourcesView()
          }
        }
        Divider().opacity(0.5)
        HStack {
          Image(
            systemName: model.status?.activity.healthy == true
              ? "checkmark.shield" : "shield.lefthalf.filled"
          ).foregroundStyle(model.status?.activity.healthy == true ? .green : .secondary)
          Text(
            model.status?.settings.idleEnabled == true
              ? "Idle protection enabled" : "Automatic shutdown disabled")
          Spacer()
          if model.busy {
            ProgressView().controlSize(.mini)
            Text("Working…")
          }
          Text("DockerD 0.1.0").foregroundStyle(.tertiary)
        }.font(.system(size: 10)).foregroundStyle(.secondary).padding(.horizontal, 30).frame(
          height: 34)
      }.background(Color(nsColor: .windowBackgroundColor))
        .overlay(alignment: .bottom) {
          if let toast = model.toast {
            Label(toast, systemImage: "checkmark.circle.fill").font(.callout).padding(12)
              .background(.regularMaterial, in: Capsule()).shadow(
                color: .black.opacity(0.1), radius: 15
              ).padding(.bottom, 48)
          }
        }
    }
    .frame(minWidth: 1000, minHeight: 700)
    .tint(accent)
    .alert(
      "DockerD",
      isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })
    ) {
      Button("OK") { model.error = nil }
    } message: {
      Text(model.error ?? "")
    }
    .confirmationDialog("Stop the engine?", isPresented: $confirmStop) {
      Button("Stop engine", role: .destructive) { Task { await model.engine("stop") } }
    } message: {
      Text(
        "Running applications and sessions will be interrupted. Images, volumes, and cluster data will be preserved."
      )
    }
    .sheet(
      isPresented: Binding(
        get: { model.diagnostics != nil }, set: { if !$0 { model.diagnostics = nil } })
    ) {
      VStack(alignment: .leading, spacing: 16) {
        HStack {
          Text("Runtime diagnostics").font(.title2.bold())
          Spacer()
          Button("Done") { model.diagnostics = nil }
        }
        ScrollView {
          Text(model.diagnostics?.isEmpty == false ? model.diagnostics! : "No runtime logs yet.")
            .font(.system(size: 11, design: .monospaced)).textSelection(.enabled).frame(
              maxWidth: .infinity, alignment: .leading)
        }
        Button("Open logs folder") { NSWorkspace.shared.open(model.home) }
      }.padding(24).frame(width: 800, height: 550)
    }
  }
  private func sidebarButton(_ section: Section) -> some View {
    Button {
      model.section = section
    } label: {
      HStack(spacing: 11) {
        Image(systemName: section.symbol).font(.system(size: 15)).frame(width: 20)
        Text(section.rawValue).font(
          .system(size: 13, weight: model.section == section ? .semibold : .regular))
        Spacer()
        if section == .overview {
          Circle().fill(.green).frame(width: 5, height: 5).opacity(
            model.status?.running == true ? 1 : 0)
        }
      }
      .foregroundStyle(model.section == section ? accent : Color.primary.opacity(0.72)).padding(
        .horizontal, 13
      ).frame(height: 39)
      .background(
        model.section == section ? accent.opacity(0.10) : .clear,
        in: RoundedRectangle(cornerRadius: 8)
      )
      .contentShape(Rectangle())
    }.buttonStyle(.plain).padding(.horizontal, 12).padding(.vertical, 2).accessibilityAddTraits(
      model.section == section ? .isSelected : [])
  }
}

struct EmptyPanel<Actions: View>: View {
  let symbol: String
  let title: String
  let description: String
  @ViewBuilder var actions: Actions
  var body: some View {
    VStack(spacing: 16) {
      Spacer()
      Image(systemName: symbol).font(.system(size: 42, weight: .light)).foregroundStyle(accent)
        .padding(22).background(accent.opacity(0.07), in: RoundedRectangle(cornerRadius: 24))
      Text(title).font(.title2.weight(.semibold))
      Text(description).font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center)
        .frame(maxWidth: 430)
      actions
      Spacer()
    }.frame(maxWidth: .infinity).padding(30)
  }
}

struct OverviewView: View {
  @EnvironmentObject private var model: AppModel
  var body: some View {
    ScrollView {
      VStack(spacing: 20) {
        hero
        HStack(spacing: 16) {
          if model.status?.running == true, let usage = model.status?.metrics {
            metric(
              "CPU usage", value: String(format: "%.0f", usage.cpuPercent), unit: "%", icon: "cpu")
            metric(
              "Memory used", value: String(format: "%.1f", usage.memoryUsedGiB), unit: "GiB",
              icon: "memorychip")
            metric(
              "Disk used", value: String(format: "%.1f", usage.diskUsedGiB), unit: "GiB",
              icon: "externaldrive")
          } else {
            metric(
              "CPU allocation", value: "\(model.status?.settings.cpus ?? 4)", unit: "cores",
              icon: "cpu")
            metric(
              "Memory allocation", value: "\(model.status?.settings.memoryGiB ?? 4)", unit: "GiB",
              icon: "memorychip")
            metric(
              "Disk capacity", value: "\(model.status?.settings.diskGiB ?? 60)", unit: "GiB",
              icon: "externaldrive")
          }
        }
        Surface {
          VStack(alignment: .leading, spacing: 18) {
            HStack {
              Image(systemName: "leaf.fill").foregroundStyle(.green)
              Text("Automatic idle shutdown").font(.system(size: 16, weight: .semibold))
              Spacer()
              Text(model.status?.settings.idleEnabled == true ? "ENABLED" : "DISABLED").font(
                .system(size: 9, weight: .bold)
              ).tracking(1.2).foregroundStyle(.secondary)
            }
            Text(
              "Your engine rests when your work does. Running workloads, open terminals, and port forwards keep it awake."
            ).font(.callout).foregroundStyle(.secondary)
            Divider()
            HStack(alignment: .top) {
              VStack(alignment: .leading, spacing: 5) {
                Text("IDLE TIMEOUT").font(.system(size: 9, weight: .semibold)).tracking(1)
                  .foregroundStyle(.secondary)
                Text("\(model.status?.settings.idleMinutes ?? 10) minutes").font(
                  .system(size: 22, weight: .medium))
              }
              Spacer()
              VStack(alignment: .leading, spacing: 5) {
                Text("WARNING BEFORE STOP").font(.system(size: 9, weight: .semibold)).tracking(1)
                  .foregroundStyle(.secondary)
                Text("\(model.status?.settings.countdownSeconds ?? 60) seconds").font(
                  .system(size: 22, weight: .medium))
              }
              Spacer()
              Button("Configure") { model.section = .settings }.controlSize(.large)
            }
            if let deadline = model.status?.activity.deadlineDate {
              HStack {
                Image(systemName: "timer")
                Text("Engine stops in")
                Text(deadline, style: .timer).monospacedDigit()
                Spacer()
                Button("Keep running") { Task { await model.resetIdle() } }
              }.font(.callout).padding(12).background(
                accent.opacity(0.07), in: RoundedRectangle(cornerRadius: 8))
            }
          }
        }
        Surface {
          VStack(alignment: .leading, spacing: 16) {
            HStack {
              Text("What’s keeping the engine awake").font(.system(size: 15, weight: .semibold))
              Spacer()
              Text("\(model.status?.activity.blockers.count ?? 0)").font(.caption.monospacedDigit())
                .padding(.horizontal, 8).padding(.vertical, 3).background(
                  .quaternary, in: Capsule())
            }
            if let blockers = model.status?.activity.blockers, !blockers.isEmpty {
              ForEach(Array(blockers.enumerated()), id: \.offset) { _, blocker in
                HStack(spacing: 12) {
                  Image(systemName: blocker.symbol).foregroundStyle(accent).frame(width: 28)
                  VStack(alignment: .leading, spacing: 4) {
                    Text(blocker.name).font(.system(size: 12, weight: .medium))
                    Text(blocker.detail).font(.system(size: 11)).foregroundStyle(.secondary)
                      .lineLimit(2)
                  }
                  Spacer()
                  Text(blocker.kind.uppercased()).font(.system(size: 8, weight: .semibold))
                    .foregroundStyle(.tertiary)
                }
              }
            } else {
              Label(
                model.status?.running == true
                  ? "No active work. DockerD will follow your idle policy."
                  : "Start your engine to monitor activity.", systemImage: "checkmark.circle"
              ).font(.callout).foregroundStyle(.secondary).padding(.vertical, 6)
            }
          }
        }
        HStack {
          Label("Your data stays on this Mac", systemImage: "lock.shield")
          Spacer()
          Button("View diagnostics") { Task { await model.showDiagnostics() } }.buttonStyle(.plain)
            .foregroundStyle(accent)
        }.font(.caption).foregroundStyle(.secondary).padding(.horizontal, 4)
      }.padding(.horizontal, 30).padding(.bottom, 30)
    }
  }
  private var hero: some View {
    HStack(spacing: 24) {
      VStack(alignment: .leading, spacing: 12) {
        Text(
          model.status?.running == true
            ? "READY TO BUILD"
            : model.status?.changing == true ? "GETTING THINGS READY" : "YOUR LOCAL ENGINE"
        ).font(.system(size: 10, weight: .bold)).tracking(1.7).foregroundStyle(.white.opacity(0.65))
        Text(
          model.status?.running == true
            ? "A little less overhead.\nA lot more possibility."
            : model.status?.changing == true
              ? "Making room\nfor your next idea." : "Everything you need.\nOnly when you need it."
        ).font(.system(size: 29, weight: .semibold)).tracking(-0.6).foregroundStyle(.white)
          .fixedSize(horizontal: false, vertical: true)
        if let failure = model.status?.error {
          Text(failure).font(.callout).foregroundStyle(.white.opacity(0.9)).lineLimit(4)
        } else {
          Text(
            model.status?.message.isEmpty == false
              ? model.status!.message : "Docker and Kubernetes, with a quieter footprint."
          ).font(.system(size: 12)).foregroundStyle(.white.opacity(0.7)).lineLimit(3)
        }
        if model.status?.changing == true { ProgressView().controlSize(.small).tint(.white) }
        if model.status?.vmExists == false {
          Text("First start downloads Linux and sets up your private VM.").font(.caption)
            .foregroundStyle(.white.opacity(0.6))
        }
      }
      Spacer(minLength: 0)
      ZStack {
        Circle().stroke(.white.opacity(0.07), lineWidth: 1).frame(width: 180, height: 180)
        Circle().stroke(.white.opacity(0.1), lineWidth: 1).frame(width: 140, height: 140)
        Image(systemName: "shippingbox.fill").font(.system(size: 72, weight: .light))
          .foregroundStyle(
            LinearGradient(
              colors: [.white, .white.opacity(0.55)], startPoint: .topLeading,
              endPoint: .bottomTrailing)
          ).rotationEffect(.degrees(-8))
        Image(systemName: "leaf.fill").font(.system(size: 21)).foregroundStyle(
          Color(red: 0.62, green: 0.96, blue: 0.78)
        ).padding(14).background(
          Color(red: 0.14, green: 0.34, blue: 0.44), in: RoundedRectangle(cornerRadius: 17)
        ).rotationEffect(.degrees(12)).offset(x: 57, y: 51)
      }.frame(width: 185).accessibilityHidden(true)
    }.padding(30).frame(maxWidth: .infinity, minHeight: 230, alignment: .leading)
      .background(
        LinearGradient(
          colors: [
            Color(red: 0.11, green: 0.19, blue: 0.34), Color(red: 0.15, green: 0.31, blue: 0.54),
          ], startPoint: .leading, endPoint: .trailing), in: RoundedRectangle(cornerRadius: 18))
  }
  private func metric(_ title: String, value: String, unit: String, icon: String) -> some View {
    Surface {
      VStack(alignment: .leading, spacing: 14) {
        HStack {
          Text(title).font(.system(size: 11)).foregroundStyle(.secondary)
          Spacer()
          Image(systemName: icon).foregroundStyle(.tertiary)
        }
        HStack(alignment: .firstTextBaseline, spacing: 6) {
          Text(value).font(.system(size: 27, weight: .semibold))
          Text(unit).font(.callout).foregroundStyle(.secondary)
        }
      }
    }
  }
}

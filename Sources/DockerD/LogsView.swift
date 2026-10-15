import SwiftUI

struct LogsView: View {
  @EnvironmentObject private var model: AppModel
  @State private var source = LogSource.service
  @State private var snapshot = LogSnapshot(text: "", truncated: false)
  @State private var filter = ""
  @State private var live = true
  @State private var follow = true
  @State private var updated: Date?
  @State private var failure: String?
  @State private var refreshing = false

  private var visibleText: String {
    guard !filter.isEmpty else { return snapshot.text }
    return snapshot.text.components(separatedBy: .newlines)
      .filter { $0.localizedCaseInsensitiveContains(filter) }.joined(separator: "\n")
  }

  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      if let error = model.status?.error {
        Label(error, systemImage: "exclamationmark.triangle")
          .font(.callout).foregroundStyle(.red).textSelection(.enabled)
      }
      HStack {
        Picker("Log source", selection: $source) {
          ForEach(LogSource.allCases) { Text($0.rawValue).tag($0) }
        }.pickerStyle(.segmented)
        Button {
          Task { await refresh() }
        } label: {
          Label("Refresh", systemImage: "arrow.clockwise")
        }.disabled(refreshing).help("Refresh logs")
      }
      HStack {
        TextField("Filter log lines…", text: $filter).textFieldStyle(.roundedBorder)
        Toggle("Live", isOn: $live).toggleStyle(.switch).controlSize(.small)
        Toggle("Follow latest", isOn: $follow).toggleStyle(.checkbox)
      }
      if let failure {
        Label(failure, systemImage: "exclamationmark.triangle").foregroundStyle(.red)
          .font(.caption).textSelection(.enabled)
      }
      ScrollViewReader { proxy in
        ScrollView([.horizontal, .vertical]) {
          VStack(alignment: .leading, spacing: 0) {
            Text(
              snapshot.text.isEmpty
                ? "No entries yet for this log."
                : visibleText.isEmpty ? "No lines match your filter." : visibleText
            ).font(.system(size: 11, design: .monospaced)).textSelection(.enabled)
              .fixedSize(horizontal: true, vertical: true)
            Color.clear.frame(height: 1).id("latest")
          }.padding(16)
        }
        .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(.primary.opacity(0.08)))
        .onChange(of: snapshot.text) { _, _ in
          if follow { proxy.scrollTo("latest", anchor: .bottomLeading) }
        }
        .onChange(of: follow) { _, enabled in
          if enabled { proxy.scrollTo("latest", anchor: .bottomLeading) }
        }
      }
      HStack {
        VStack(alignment: .leading, spacing: 4) {
          Text(source.path).font(.system(size: 10, design: .monospaced))
          if let updated {
            Text("Updated \(updated.formatted(date: .omitted, time: .standard))")
          }
          if snapshot.truncated {
            Text("Showing the latest 128 KiB. Open the folder for the full log.")
          }
        }.font(.caption).foregroundStyle(.secondary)
        Spacer()
        Button("Open logs folder") { NSWorkspace.shared.open(model.home) }
        Button("Copy displayed logs") {
          NSPasteboard.general.clearContents()
          NSPasteboard.general.setString(visibleText, forType: .string)
          model.showToast("Logs copied")
        }.disabled(visibleText.isEmpty)
      }
    }.padding(.horizontal, 30).padding(.bottom, 24)
      .task(id: source) {
        snapshot = LogSnapshot(text: "", truncated: false)
        updated = nil
        await refresh()
        while !Task.isCancelled {
          do { try await Task.sleep(for: .seconds(3)) } catch { return }
          if live { await refresh() }
        }
      }
  }

  private func refresh() async {
    let selected = source
    let url = model.home.appendingPathComponent(selected.path)
    refreshing = true
    defer { refreshing = false }
    do {
      let value = try await Task.detached(priority: .utility) { try LogReader.read(url) }.value
      guard !Task.isCancelled, source == selected else { return }
      snapshot = value
      failure = nil
      updated = Date()
    } catch {
      guard !Task.isCancelled, source == selected else { return }
      failure = "Could not read \(selected.path): \(error.localizedDescription)"
    }
  }
}

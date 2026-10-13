import SwiftUI
import ServiceManagement
import UserNotifications

struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @State private var draft = EngineSettings()
    @State private var launchAtLogin = SMAppService.mainApp.status == .enabled
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("notifications") private var notifications = false
    @State private var confirmContext = false
    var runtimeEditable: Bool { model.status?.state == "stopped" }
    var body: some View {
        ScrollView {
            VStack(spacing: 20) {
                Surface {
                    VStack(alignment: .leading, spacing: 20) {
                        heading("Idle protection", "leaf.fill", .green)
                        Toggle("Turn off the engine when idle", isOn: $draft.idleEnabled).toggleStyle(.switch)
                        Text("Running applications, pending pods, open exec sessions, port forwards, and engine operations prevent shutdown. System infrastructure is excluded automatically.").font(.callout).foregroundStyle(.secondary)
                        HStack { Stepper("Idle timeout: \(draft.idleMinutes) min", value: $draft.idleMinutes, in: 1...1440); Spacer(minLength: 30); Stepper("Countdown: \(draft.countdownSeconds) sec", value: $draft.countdownSeconds, in: 10...300, step: 10) }.disabled(!draft.idleEnabled)
                        Label("Restart is manual. Monitoring uncertainty always prevents automatic shutdown.", systemImage: "info.circle").font(.caption).foregroundStyle(.secondary)
                    }
                }
                Surface {
                    VStack(alignment: .leading, spacing: 20) {
                        heading("Engine resources", "cpu", accent)
                        if !runtimeEditable { Label("Stop the engine to change runtime settings.", systemImage: "pause.circle").font(.caption).foregroundStyle(.orange) }
                        HStack { Stepper("CPU cores: \(draft.cpus)", value: $draft.cpus, in: 1...ProcessInfo.processInfo.processorCount); Spacer(minLength: 40); Stepper("Memory: \(draft.memoryGiB) GiB", value: $draft.memoryGiB, in: 2...max(2, Int(ProcessInfo.processInfo.physicalMemory / 1_073_741_824 / 2))) }.disabled(!runtimeEditable)
                        Stepper("Disk capacity: \(draft.diskGiB) GiB", value: $draft.diskGiB, in: 20...1024, step: 10).disabled(!runtimeEditable || model.status?.vmExists == true)
                        Text("The virtual disk grows as needed. Capacity is fixed after the first start.").font(.caption).foregroundStyle(.secondary)
                        Divider()
                        Toggle("Enable local Kubernetes", isOn: $draft.kubernetes).toggleStyle(.switch).disabled(!runtimeEditable)
                    }
                }
                Surface {
                    VStack(alignment: .leading, spacing: 14) {
                        heading("Shared folders", "folder", accent)
                        Text("Only these folders are mounted read-write in the Linux VM. Add project folders used by bind mounts.").font(.callout).foregroundStyle(.secondary)
                        ForEach(draft.sharedFolders, id: \.self) { path in HStack { Image(systemName: "folder").foregroundStyle(.secondary); Text(path).font(.system(size: 11, design: .monospaced)).lineLimit(1); Spacer(); Button { draft.sharedFolders.removeAll { $0 == path } } label: { Image(systemName: "minus.circle") }.disabled(!runtimeEditable) } }
                        Button("Add folder…") { let panel = NSOpenPanel(); panel.canChooseDirectories = true; panel.canChooseFiles = false; panel.allowsMultipleSelection = true; if panel.runModal() == .OK { for url in panel.urls where !draft.sharedFolders.contains(url.path) { draft.sharedFolders.append(url.path) } } }.disabled(!runtimeEditable)
                    }
                }
                HStack { Text(draft == model.status?.settings ? "All engine settings are saved" : "You have unsaved changes").font(.caption).foregroundStyle(.secondary); Spacer(); Button("Reset changes") { if let current = model.status?.settings { draft = current } }; Button("Save engine settings") { Task { await model.save(draft) } }.buttonStyle(.borderedProminent).disabled(draft == model.status?.settings) }
                Surface {
                    VStack(alignment: .leading, spacing: 18) {
                        heading("Make yourself at home", "paintpalette", accent)
                        Picker("Appearance", selection: $appearance) { Text("System").tag("system"); Text("Light").tag("light"); Text("Dark").tag("dark") }.pickerStyle(.segmented)
                        Toggle("Launch DockerD at login", isOn: $launchAtLogin).onChange(of: launchAtLogin) { _, value in do { if value { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() } } catch { model.error = error.localizedDescription; launchAtLogin = SMAppService.mainApp.status == .enabled } }
                        Toggle("Notify me before idle shutdown", isOn: $notifications).onChange(of: notifications) { _, value in if value { Task { do { let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]); notifications = granted } catch { notifications = false; model.error = error.localizedDescription } } } }
                        Text("Opening DockerD doesn’t automatically start the engine.").font(.caption).foregroundStyle(.secondary)
                    }
                }
                Surface {
                    VStack(alignment: .leading, spacing: 16) {
                        heading("Command-line access", "terminal", accent)
                        Text("Use the dedicated Docker context without changing your other environments.").font(.callout).foregroundStyle(.secondary)
                        code("docker --context dockerd-local ps")
                        HStack { Button("Make DockerD the active Docker context…") { confirmContext = true }.disabled(model.status?.vmExists != true); Spacer() }
                        Text("Kubernetes configuration").font(.caption).foregroundStyle(.secondary)
                        code("export KUBECONFIG=\"\(model.status?.kubeconfig ?? "")\"")
                        Text("To use the bundled CLI tools, add the following to your shell configuration:").font(.caption).foregroundStyle(.secondary)
                        code("export PATH=\"\(Bundle.main.resourceURL!.path)/Runtime/bin:\(Bundle.main.resourceURL!.path)/Runtime/docker:$PATH\"\nexport DOCKER_CLI_PLUGIN_EXTRA_DIRS=\"\(Bundle.main.resourceURL!.path)/Runtime/cli-plugins\"")
                    }
                }
                HStack { Button("Open application data") { NSWorkspace.shared.open(model.home) }; Button("Runtime diagnostics") { Task { await model.showDiagnostics() } }; Spacer(); Text("DockerD · Built for your Mac").font(.caption).foregroundStyle(.tertiary) }
            }.padding(.horizontal, 30).padding(.bottom, 30)
        }.onAppear { if let current = model.status?.settings { draft = current } }
            .confirmationDialog("Switch your active Docker context?", isPresented: $confirmContext) { Button("Use dockerd-local") { Task { await model.activateContext() } } } message: { Text("Future Docker commands in Terminal will target DockerD unless you explicitly select another context.") }
    }
    private func heading(_ text: String, _ icon: String, _ color: Color) -> some View { HStack(spacing: 10) { Image(systemName: icon).foregroundStyle(color); Text(text).font(.system(size: 16, weight: .semibold)) } }
    private func code(_ value: String) -> some View { HStack(alignment: .top) { Text(value).font(.system(size: 11, design: .monospaced)).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading); Button { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(value, forType: .string); model.showToast("Copied to clipboard") } label: { Image(systemName: "doc.on.doc") }.buttonStyle(.plain).help("Copy command") }.padding(12).background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 8)) }
}

import AppKit
import LaserKit
import SwiftUI

@main
struct LaserEmailApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var model = AppModel()

    var body: some Scene {
        WindowGroup("Laser Email", id: "main") {
            MainView()
                .environment(model)
                .task { await model.start() }
        }
        .defaultSize(width: 1240, height: 780)
        .commands { MailCommands(model: model) }

        WindowGroup("New Message", id: "compose", for: ComposeRequest.self) { $request in
            ComposeView(request: request ?? ComposeRequest(kind: .new))
                .environment(model)
        }
        .defaultSize(width: 720, height: 560)

        Settings {
            SettingsView()
                .environment(model)
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}

/// The menu bar: Mail's familiar shortcuts.
struct MailCommands: Commands {
    let model: AppModel
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        CommandGroup(after: .appSettings) {
            Button("Add Account…") { model.showAddAccount = true }
        }
        CommandGroup(replacing: .newItem) {
            Button("New Message") { openWindow(id: "compose", value: ComposeRequest(kind: .new)) }
                .keyboardShortcut("n")
        }
        CommandMenu("Mailbox") {
            Button("Get All New Mail") { Task { await model.getMail() } }
                .keyboardShortcut("n", modifiers: [.command, .shift])
            Divider()
            Button("Show Only Unread") { model.unreadOnly.toggle() }
                .keyboardShortcut("u", modifiers: [.command, .option])
        }
        CommandMenu("Message") {
            Button("Reply") { compose(.reply) }.keyboardShortcut("r")
            Button("Reply All") { compose(.replyAll) }.keyboardShortcut("r", modifiers: [.command, .shift])
            Button("Forward") { compose(.forward) }.keyboardShortcut("f", modifiers: [.command, .shift])
            Divider()
            Button("Archive") { Task { await model.perform("archive") } }.keyboardShortcut("a", modifiers: [.command, .control])
            Button("Move to Trash") { Task { await model.perform("delete") } }.keyboardShortcut(.delete, modifiers: [.command])
            Button("Move to Junk") { Task { await model.perform("junk") } }.keyboardShortcut("j", modifiers: [.command, .shift])
            Divider()
            Button("Mark as Read/Unread") { Task { await model.toggleRead() } }.keyboardShortcut("u", modifiers: [.command, .shift])
            Button("Flag") { Task { await model.toggleFlag() } }.keyboardShortcut("l", modifiers: [.command, .shift])
            Divider()
            Button("Load Images") { Task { await model.loadImages(always: false) } }
        }
    }

    private func compose(_ kind: ComposeRequest.Kind) {
        guard let id = model.selectedMessageID else { return }
        openWindow(id: "compose", value: ComposeRequest(kind: kind, messageID: id))
    }
}

import LaserKit
import SwiftUI

struct MessageListView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        @Bindable var model = model
        Group {
            if model.accounts.isEmpty && model.connected {
                ContentUnavailableView {
                    Label("No Accounts", systemImage: "envelope")
                } description: {
                    Text("Add your Gmail and iCloud accounts. Everything stays on this Mac.")
                } actions: {
                    Button("Add Account…") { model.showAddAccount = true }
                }
            } else if model.messages.isEmpty {
                ContentUnavailableView(model.searchText.isEmpty ? "No Messages" : "No Results", systemImage: model.searchText.isEmpty ? "tray" : "magnifyingglass")
            } else {
                List(model.messages, selection: $model.selectedMessageID) { m in
                    MessageRow(message: m, accountLabel: showAccount ? model.account(m.accountId)?.title : nil)
                        .tag(m.id)
                        .draggable(String(m.id))
                        .swipeActions(edge: .leading) {
                            Button { Task { await model.toggleRead(m.id) } } label: {
                                Label(m.seen ? "Unread" : "Read", systemImage: m.seen ? "envelope.badge" : "envelope.open")
                            }.tint(.blue)
                        }
                        .swipeActions(edge: .trailing) {
                            Button(role: .destructive) { Task { await model.perform("delete", m.id) } } label: { Label("Trash", systemImage: "trash") }
                            Button { Task { await model.toggleFlag(m.id) } } label: { Label("Flag", systemImage: "flag") }.tint(.orange)
                            Button { Task { await model.perform("archive", m.id) } } label: { Label("Archive", systemImage: "archivebox") }.tint(.purple)
                        }
                        .contextMenu { contextMenu(m) }
                }
                .listStyle(.inset)
                .alternatingRowBackgrounds(.disabled)
            }
        }
        .navigationTitle(model.listTitle)
        .navigationSubtitle(model.listSubtitle)
        .toolbar {
            ToolbarItemGroup(placement: .navigation) {
                Button { Task { await model.getMail() } } label: { Label("Get Mail", systemImage: "envelope") }
                    .help("Get new messages")
                Button { openWindow(id: "compose", value: ComposeRequest(kind: .new)) } label: { Label("New Message", systemImage: "square.and.pencil") }
                    .help("Write a new message")
            }
            ToolbarItem {
                Toggle(isOn: $model.unreadOnly) { Label("Unread", systemImage: model.unreadOnly ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle") }
                    .help("Show only unread messages")
            }
        }
    }

    /// In unified views each row says which account it came from.
    private var showAccount: Bool {
        guard model.accounts.count > 1 else { return false }
        if case .mailbox = model.selection { return model.searchText.isEmpty == false }
        return true
    }

    @ViewBuilder private func contextMenu(_ m: Message) -> some View {
        Button("Reply") { openWindow(id: "compose", value: ComposeRequest(kind: .reply, messageID: m.id)) }
        Button("Reply All") { openWindow(id: "compose", value: ComposeRequest(kind: .replyAll, messageID: m.id)) }
        Button("Forward") { openWindow(id: "compose", value: ComposeRequest(kind: .forward, messageID: m.id)) }
        Divider()
        Button(m.seen ? "Mark as Unread" : "Mark as Read") { Task { await model.toggleRead(m.id) } }
        Button(m.flagged ? "Unflag" : "Flag") { Task { await model.toggleFlag(m.id) } }
        Divider()
        Menu("Move to") {
            ForEach(model.mailboxes(of: m.accountId)) { mb in
                Button(mb.name) { Task { await model.move(m.id, to: mb.id) } }
            }
        }
        Button("Archive") { Task { await model.perform("archive", m.id) } }
        Button("Move to Junk") { Task { await model.perform("junk", m.id) } }
        Button("Move to Trash") { Task { await model.perform("delete", m.id) } }
    }
}

struct MessageRow: View {
    let message: Message
    var accountLabel: String?

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            VStack(spacing: 6) {
                Circle()
                    .fill(message.seen ? Color.clear : Color.accentColor)
                    .frame(width: 9, height: 9)
                if message.answered {
                    Image(systemName: "arrowshape.turn.up.left.fill")
                        .font(.system(size: 8))
                        .foregroundStyle(.secondary)
                }
            }
            .frame(width: 12)
            .padding(.top, 4)

            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline) {
                    Text(message.from.display)
                        .font(.system(size: 13, weight: message.seen ? .semibold : .bold))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if message.hasAttachments {
                        Image(systemName: "paperclip").font(.caption).foregroundStyle(.secondary)
                    }
                    Text(listDate(message.date))
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
                HStack(alignment: .firstTextBaseline) {
                    Text(message.displaySubject)
                        .font(.system(size: 12))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if message.flagged {
                        Image(systemName: "flag.fill").font(.caption).foregroundStyle(.orange)
                    }
                }
                Text(message.snippet.isEmpty ? " " : message.snippet)
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                if let accountLabel {
                    Text(accountLabel)
                        .font(.system(size: 10, weight: .medium))
                        .foregroundStyle(.tertiary)
                }
            }
        }
        .padding(.vertical, 5)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(message.seen ? "" : "Unread, ")\(message.from.display), \(message.displaySubject)")
    }
}

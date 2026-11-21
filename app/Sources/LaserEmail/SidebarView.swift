import LaserKit
import SwiftUI

func icon(forRole role: String) -> String {
    switch role {
    case "inbox": return "tray"
    case "drafts": return "doc"
    case "sent": return "paperplane"
    case "junk": return "xmark.bin"
    case "trash": return "trash"
    case "archive": return "archivebox"
    case "all": return "tray.full"
    default: return "folder"
    }
}

struct SidebarView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        List(selection: $model.selection) {
            Section("Favorites") {
                Label("All Inboxes", systemImage: "tray.2")
                    .badge(model.counts["inbox"] ?? 0)
                    .tag(SidebarItem.allInboxes)
                Label("Flagged", systemImage: "flag")
                    .badge(model.counts["flagged"] ?? 0)
                    .tag(SidebarItem.flagged)
                Label("Drafts", systemImage: "doc")
                    .tag(SidebarItem.role("drafts"))
                Label("Sent", systemImage: "paperplane")
                    .tag(SidebarItem.role("sent"))
            }
            ForEach(model.accounts) { account in
                Section {
                    ForEach(model.mailboxes(of: account.id)) { mb in
                        Label(mb.name, systemImage: icon(forRole: mb.role))
                            .badge(mb.role == "inbox" || mb.role == "" || mb.role == "junk" ? mb.unread : 0)
                            .tag(SidebarItem.mailbox(mb.id))
                            .dropDestination(for: String.self) { items, _ in
                                for s in items { if let id = Int64(s) { Task { await model.move(id, to: mb.id) } } }
                                return true
                            }
                    }
                } header: {
                    AccountHeader(account: account, status: model.status[account.id])
                }
            }
        }
        .listStyle(.sidebar)
        .safeAreaInset(edge: .bottom) {
            HStack {
                Button { model.showAddAccount = true } label: { Image(systemName: "plus") }
                    .buttonStyle(.borderless)
                    .help("Add Account")
                Spacer()
            }
            .padding(8)
        }
    }
}

struct AccountHeader: View {
    let account: Account
    let status: AccountStatus?

    var body: some View {
        HStack(spacing: 4) {
            Text(account.title)
            Spacer()
            switch status?.state {
            case "connecting", "syncing":
                ProgressView().controlSize(.mini)
            case "error":
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.yellow)
                    .help(status?.error ?? "Can't connect")
            default:
                EmptyView()
            }
        }
        .help(account.email)
    }
}

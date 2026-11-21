import LaserKit
import SwiftUI
import UniformTypeIdentifiers

struct ComposeRequest: Codable, Hashable {
    enum Kind: String, Codable { case new, reply, replyAll, forward }
    var kind: Kind
    var messageID: Int64?
    var token = UUID() // every request opens its own window
}

struct ComposeView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let request: ComposeRequest

    @State private var accountID: Int64 = 0
    @State private var to = ""
    @State private var cc = ""
    @State private var bcc = ""
    @State private var showBcc = false
    @State private var subject = ""
    @State private var text = ""
    @State private var inReplyTo: String?
    @State private var references: [String]?
    @State private var files: [URL] = []
    @State private var sending = false
    @State private var error: String?
    @State private var picking = false
    @State private var loaded = false

    var body: some View {
        VStack(spacing: 0) {
            Grid(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 0) {
                field("To:", text: $to)
                field("Cc:", text: $cc)
                if showBcc { field("Bcc:", text: $bcc) }
                field("Subject:", text: $subject)
                if model.accounts.count > 1 {
                    GridRow {
                        Text("From:").foregroundStyle(.secondary).gridColumnAlignment(.trailing)
                        Picker("", selection: $accountID) {
                            ForEach(model.accounts) { a in
                                Text(a.name.isEmpty ? a.email : "\(a.name) – \(a.email)").tag(a.id)
                            }
                        }
                        .labelsHidden()
                        .fixedSize()
                        .padding(.vertical, 4)
                    }
                    Divider().gridCellUnsizedAxes(.horizontal)
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 6)

            if !files.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(files, id: \.self) { f in
                            HStack(spacing: 4) {
                                Image(systemName: "doc")
                                Text(f.lastPathComponent).lineLimit(1)
                                Button { files.removeAll { $0 == f } } label: { Image(systemName: "xmark.circle.fill") }
                                    .buttonStyle(.borderless)
                            }
                            .font(.system(size: 12))
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(.quaternary.opacity(0.5), in: Capsule())
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                }
            }

            TextEditor(text: $text)
                .font(.system(size: 13))
                .scrollContentBackground(.hidden)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
        }
        .navigationTitle(subject.isEmpty ? "New Message" : subject)
        .toolbar {
            ToolbarItem(placement: .navigation) {
                Button { Task { await send() } } label: { Label("Send", systemImage: "paperplane") }
                    .keyboardShortcut("d", modifiers: [.command, .shift])
                    .disabled(sending || Draft.parseAddresses(to + "," + cc + "," + bcc).isEmpty)
                    .help("Send (⇧⌘D)")
            }
            ToolbarItemGroup(placement: .primaryAction) {
                Button { picking = true } label: { Label("Attach", systemImage: "paperclip") }
                    .help("Attach files")
                Toggle(isOn: $showBcc) { Label("Bcc", systemImage: "person.2.badge.plus") }
                    .help("Show the Bcc field")
            }
        }
        .fileImporter(isPresented: $picking, allowedContentTypes: [.item], allowsMultipleSelection: true) { r in
            if case .success(let urls) = r { files.append(contentsOf: urls) }
        }
        .overlay {
            if sending { ProgressView("Sending…").padding().background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10)) }
        }
        .alert("Couldn't send", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button("OK") { error = nil }
        } message: { Text(error ?? "") }
        .task { await load() }
        .frame(minWidth: 520, minHeight: 360)
    }

    private func field(_ label: String, text: Binding<String>) -> some View {
        Group {
            GridRow {
                Text(label).foregroundStyle(.secondary).gridColumnAlignment(.trailing)
                TextField("", text: text).textFieldStyle(.plain).padding(.vertical, 6)
            }
            Divider().gridCellUnsizedAxes(.horizontal)
        }
    }

    private func load() async {
        guard !loaded else { return }
        loaded = true
        accountID = defaultAccount()
        guard request.kind != .new, let id = request.messageID else { return }
        let op = ["reply": "reply", "replyAll": "reply_all", "forward": "forward"][request.kind.rawValue]!
        do {
            let r = try await model.client.draft(op, for: id)
            accountID = r.accountId
            to = Draft.formatAddresses(r.draft.to)
            cc = Draft.formatAddresses(r.draft.cc)
            subject = r.draft.subject
            text = r.draft.text
            inReplyTo = r.draft.inReplyTo
            references = r.draft.references
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// The account of the mailbox being viewed, else the first one.
    private func defaultAccount() -> Int64 {
        if case .mailbox(let id) = model.selection, let mb = model.mailboxes.first(where: { $0.id == id }) { return mb.accountId }
        return model.accounts.first?.id ?? 0
    }

    private func send() async {
        var d = Draft(to: Draft.parseAddresses(to), cc: Draft.parseAddresses(cc), bcc: Draft.parseAddresses(bcc), subject: subject, text: text)
        d.inReplyTo = inReplyTo
        d.references = references
        sending = true
        defer { sending = false }
        do {
            try await model.client.send(account: accountID, draft: d, attachPaths: files.map(\.path))
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

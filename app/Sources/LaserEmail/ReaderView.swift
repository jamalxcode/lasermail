import LaserKit
import SwiftUI

struct ReaderView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Group {
            if let v = model.openMessage, v.message.id == model.selectedMessageID {
                VStack(spacing: 0) {
                    HeaderView(view: v)
                    if let s = privacySummary(v.privacy, remoteLoaded: v.remoteImages) {
                        PrivacyBanner(text: s, canLoad: v.privacy.remoteBlocked > 0 && !v.remoteImages, trackers: v.privacy.trackers)
                    }
                    if v.readReceiptRequested {
                        Label("The sender asked for a read receipt. Laser never sends one.", systemImage: "eye.slash")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 20)
                            .padding(.vertical, 4)
                    }
                    if !v.files.isEmpty {
                        AttachmentStrip(files: v.files)
                    }
                    Divider()
                    MailWebView(html: v.html, remoteImages: v.remoteImages, messageID: v.message.id, client: model.client) { id in
                        model.renderedMessageID = id
                    }
                }
            } else if model.selectedMessageID != nil && model.loadingMessage {
                ProgressView()
            } else {
                ContentUnavailableView("No Message Selected", systemImage: "envelope")
            }
        }
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                let none = model.selectedMessageID == nil
                Button { Task { await model.perform("archive") } } label: { Label("Archive", systemImage: "archivebox") }
                    .help("Archive").disabled(none)
                Button { Task { await model.perform("delete") } } label: { Label("Trash", systemImage: "trash") }
                    .help("Move to Trash").disabled(none)
                Button { Task { await model.perform("junk") } } label: { Label("Junk", systemImage: "xmark.bin") }
                    .help("Move to Junk").disabled(none)
            }
            ToolbarItemGroup(placement: .primaryAction) {
                let none = model.selectedMessageID == nil
                Button { compose(.reply) } label: { Label("Reply", systemImage: "arrowshape.turn.up.left") }
                    .help("Reply").disabled(none)
                Button { compose(.replyAll) } label: { Label("Reply All", systemImage: "arrowshape.turn.up.left.2") }
                    .help("Reply All").disabled(none)
                Button { compose(.forward) } label: { Label("Forward", systemImage: "arrowshape.turn.up.right") }
                    .help("Forward").disabled(none)
                Button { Task { await model.toggleFlag() } } label: {
                    Label("Flag", systemImage: model.selectedMessage?.flagged == true ? "flag.fill" : "flag")
                }
                .help("Flag").disabled(none)
            }
        }
    }

    private func compose(_ kind: ComposeRequest.Kind) {
        guard let id = model.selectedMessageID else { return }
        openWindow(id: "compose", value: ComposeRequest(kind: kind, messageID: id))
    }
}

/// "9:41 AM" today, "Yesterday 9:41 AM", else "Nov 3, 2026 9:41 AM".
func headerDate(_ d: Date) -> String {
    let time = d.formatted(date: .omitted, time: .shortened)
    if Calendar.current.isDateInToday(d) { return time }
    if Calendar.current.isDateInYesterday(d) { return "Yesterday " + time }
    return d.formatted(date: .abbreviated, time: .omitted) + " " + time
}

struct Avatar: View {
    let address: Address
    var size: CGFloat = 40

    var body: some View {
        Circle()
            .fill(LinearGradient(colors: [Color(hue: hue(for: address.email), saturation: 0.35, brightness: 0.75),
                                          Color(hue: hue(for: address.email), saturation: 0.45, brightness: 0.6)],
                                 startPoint: .top, endPoint: .bottom))
            .frame(width: size, height: size)
            .overlay(Text(initials(address)).font(.system(size: size * 0.4, weight: .medium)).foregroundStyle(.white))
            .accessibilityHidden(true)
    }
}

struct HeaderView: View {
    let view: MessageView

    var body: some View {
        let m = view.message
        HStack(alignment: .top, spacing: 12) {
            Avatar(address: m.from)
            VStack(alignment: .leading, spacing: 3) {
                Text(m.from.display)
                    .font(.system(size: 14, weight: .semibold))
                    .textSelection(.enabled)
                    .help(m.from.email)
                Text(m.displaySubject)
                    .font(.system(size: 13, weight: .medium))
                    .textSelection(.enabled)
                Text("To: " + (m.to.isEmpty ? view.account : m.to.map(\.display).joined(separator: ", ")))
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                if !m.cc.isEmpty {
                    Text("Cc: " + m.cc.map(\.display).joined(separator: ", "))
                        .font(.system(size: 12))
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            .layoutPriority(1)
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 3) {
                Text(headerDate(m.date))
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .fixedSize()
                Text(view.mailbox)
                    .font(.system(size: 11))
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
                    .help(view.account)
                if m.flagged {
                    Image(systemName: "flag.fill").foregroundStyle(.orange)
                }
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 14)
    }
}

/// Mail's "remote content" bar, with what Laser blocked.
struct PrivacyBanner: View {
    @Environment(AppModel.self) private var model
    let text: String
    let canLoad: Bool
    let trackers: [String]

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "hand.raised.fill")
                .foregroundStyle(.blue)
            Text(text)
                .font(.system(size: 12))
                .help(trackers.isEmpty ? "" : "Trackers: " + trackers.joined(separator: ", "))
            Spacer()
            if canLoad {
                Button("Load Images") { Task { await model.loadImages(always: false) } }
                    .controlSize(.small)
                Menu {
                    Button("Always Load from This Sender") { Task { await model.loadImages(always: true) } }
                } label: { EmptyView() }
                    .menuStyle(.borderlessButton)
                    .fixedSize()
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 6)
        .background(.bar)
        .overlay(alignment: .top) { Divider() }
    }
}

struct AttachmentStrip: View {
    @Environment(AppModel.self) private var model
    let files: [Attachment]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(files) { a in
                    Button { Task { await model.saveAttachment(a) } } label: {
                        HStack(spacing: 6) {
                            Image(systemName: a.type.hasPrefix("image/") ? "photo" : a.type == "application/pdf" ? "doc.richtext" : "doc")
                            VStack(alignment: .leading, spacing: 0) {
                                Text(a.name).font(.system(size: 12)).lineLimit(1)
                                Text(byteCount(a.size)).font(.system(size: 10)).foregroundStyle(.secondary)
                            }
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 8))
                    }
                    .buttonStyle(.plain)
                    .help("Save to Downloads")
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 8)
        }
    }
}

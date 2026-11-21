import AppKit
import LaserKit
import SwiftUI

struct SettingsView: View {
    var body: some View {
        TabView {
            AccountsSettings()
                .tabItem { Label("Accounts", systemImage: "at") }
            PrivacySettings()
                .tabItem { Label("Privacy", systemImage: "hand.raised") }
        }
        .frame(width: 640, height: 420)
    }
}

struct AccountsSettings: View {
    @Environment(AppModel.self) private var model
    @State private var selected: Int64?
    @State private var confirmRemove: Account?
    @State private var newPassword = ""

    var body: some View {
        @Bindable var model = model
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                List(model.accounts, selection: $selected) { a in
                    HStack {
                        Image(systemName: "envelope.fill").foregroundStyle(.blue)
                        VStack(alignment: .leading) {
                            Text(a.title).font(.system(size: 13, weight: .medium))
                            Text(a.email).font(.system(size: 11)).foregroundStyle(.secondary)
                        }
                    }
                    .tag(a.id)
                }
                HStack(spacing: 0) {
                    Button { model.showAddAccount = true } label: { Image(systemName: "plus").frame(width: 24, height: 20) }
                    Button { confirmRemove = model.account(selected ?? -1) } label: { Image(systemName: "minus").frame(width: 24, height: 20) }
                        .disabled(selected == nil)
                    Spacer()
                }
                .buttonStyle(.borderless)
                .padding(4)
            }
            .frame(width: 220)
            Divider()
            Group {
                if let a = model.account(selected ?? -1) {
                    Form {
                        LabeledContent("Email", value: a.email)
                        LabeledContent("Type", value: a.title)
                        LabeledContent("Sign-in", value: a.auth == "oauth" ? "Google (OAuth)" : "App password, kept in your Keychain")
                        LabeledContent("Incoming", value: "\(a.settings.imap.host):\(a.settings.imap.port) (\(a.settings.imap.security.uppercased()))")
                        LabeledContent("Outgoing", value: "\(a.settings.smtp.host):\(a.settings.smtp.port) (\(a.settings.smtp.security.uppercased()))")
                        if let st = model.status[a.id] {
                            LabeledContent("Status", value: st.error ?? st.state.capitalized)
                        }
                        if a.auth != "oauth" {
                            HStack {
                                SecureField("New app password", text: $newPassword)
                                Button("Update") {
                                    let pw = newPassword
                                    Task {
                                        do { try await model.client.updatePassword(a.id, password: pw); newPassword = "" } catch { model.alert = error.localizedDescription }
                                    }
                                }
                                .disabled(newPassword.isEmpty)
                            }
                        }
                    }
                    .formStyle(.grouped)
                } else {
                    ContentUnavailableView("Select an Account", systemImage: "at")
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .onAppear { selected = selected ?? model.accounts.first?.id }
        .sheet(isPresented: $model.showAddAccount) { AddAccountSheet() }
        .confirmationDialog("Remove \(confirmRemove?.email ?? "")?", isPresented: Binding(get: { confirmRemove != nil }, set: { if !$0 { confirmRemove = nil } })) {
            Button("Remove Account", role: .destructive) {
                if let a = confirmRemove { Task { await model.removeAccount(a) } }
                selected = nil
            }
        } message: {
            Text("Its mail is deleted from this Mac and its password from your Keychain. Nothing changes on the server.")
        }
    }
}

struct PrivacySettings: View {
    var body: some View {
        Form {
            Section("Always on") {
                Label("Remote images are blocked until you choose to load them", systemImage: "photo.badge.exclamationmark")
                Label("Tracking pixels are blocked even when images load", systemImage: "eye.slash")
                Label("Tracking parameters and redirects are removed from links", systemImage: "link")
                Label("Messages never run scripts or load fonts, styles or frames from the web", systemImage: "curlybraces")
                Label("Read receipts are never sent", systemImage: "checkmark.message")
                Label("Sent mail carries no app name, time zone or computer name", systemImage: "paperplane")
                Label("No analytics, no crash reports, no avatars from the web", systemImage: "antenna.radiowaves.left.and.right.slash")
            }
            Section("Where your mail lives") {
                Text("On this Mac only, in ~/Library/Application Support/Laser Email (readable by you alone). Passwords and sign-in tokens are kept in your login Keychain. Laser Email connects only to your mail servers, and to Google when you use Sign in with Google.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
    }
}

/// Mail's "Choose a Mail account provider" sheet, then the account form.
struct AddAccountSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    enum Provider: String, CaseIterable, Identifiable {
        case gmail, icloud, imap
        var id: String { rawValue }
        var title: String { ["gmail": "Gmail", "icloud": "iCloud", "imap": "Other Mail Account…"][rawValue]! }
        var detail: String {
            ["gmail": "Gmail and Google Workspace", "icloud": "iCloud Mail (icloud.com, me.com, mac.com)", "imap": "Any IMAP and SMTP server"][rawValue]!
        }
        var symbol: String { ["gmail": "envelope.fill", "icloud": "icloud.fill", "imap": "server.rack"][rawValue]! }
    }

    @State private var provider: Provider?
    @State private var choice: Provider = .gmail
    @State private var name = NSFullUserName()
    @State private var email = ""
    @State private var password = ""
    @State private var imapHost = ""
    @State private var imapPort = "993"
    @State private var smtpHost = ""
    @State private var smtpPort = "465"
    @State private var working = false
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if let p = provider {
                form(p)
            } else {
                Text("Choose a Mail account provider…").font(.headline)
                VStack(spacing: 0) {
                    ForEach(Provider.allCases) { p in
                        Button { choice = p } label: {
                            HStack(spacing: 12) {
                                Image(systemName: choice == p ? "largecircle.fill.circle" : "circle")
                                    .foregroundStyle(choice == p ? Color.accentColor : .secondary)
                                Image(systemName: p.symbol).font(.title2).frame(width: 28)
                                VStack(alignment: .leading) {
                                    Text(p.title).font(.system(size: 14, weight: .semibold))
                                    Text(p.detail).font(.system(size: 11)).foregroundStyle(.secondary)
                                }
                                Spacer()
                            }
                            .padding(.vertical, 10)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        if p != .imap { Divider() }
                    }
                }
                .padding(.horizontal, 12)
                .background(.background, in: RoundedRectangle(cornerRadius: 8))
                .overlay(RoundedRectangle(cornerRadius: 8).stroke(.separator))
                HStack {
                    Spacer()
                    Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                    Button("Continue") { provider = choice }.keyboardShortcut(.defaultAction)
                }
            }
        }
        .padding(20)
        .frame(width: 460)
    }

    @ViewBuilder private func form(_ p: Provider) -> some View {
        Text("Add \(p == .imap ? "a Mail Account" : p.title)").font(.headline)
        Form {
            TextField("Name", text: $name, prompt: Text("Shown to people you write to"))
            TextField("Email Address", text: $email, prompt: Text(p == .icloud ? "you@icloud.com" : p == .gmail ? "you@gmail.com" : "you@example.com"))
            SecureField(p == .icloud ? "App-Specific Password" : "App Password", text: $password, prompt: Text("xxxx xxxx xxxx xxxx"))
            if p == .imap {
                TextField("Incoming (IMAP) Server", text: $imapHost, prompt: Text("imap.example.com"))
                TextField("IMAP Port (TLS)", text: $imapPort)
                TextField("Outgoing (SMTP) Server", text: $smtpHost, prompt: Text("smtp.example.com"))
                TextField("SMTP Port (465 TLS or 587 STARTTLS)", text: $smtpPort)
            }
        }
        .formStyle(.grouped)
        .disabled(working)
        Text(help(p))
            .font(.caption)
            .foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
        if let error {
            Label(error, systemImage: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
                .font(.callout)
                .fixedSize(horizontal: false, vertical: true)
        }
        HStack {
            if p != .imap, let url = URL(string: model.presets[p.rawValue]?.helpUrl ?? "") {
                Button(p == .icloud ? "Make an App-Specific Password…" : "Make an App Password…") { NSWorkspace.shared.open(url) }
                    .buttonStyle(.link)
            }
            Spacer()
            if working { ProgressView().controlSize(.small) }
            Button("Back") { provider = nil; resetForm() }.disabled(working)
            if p == .gmail && model.oauthAvailable {
                Button("Sign in with Google…") { Task { await add(p, oauth: true) } }
                    .disabled(working || email.isEmpty)
            }
            Button("Sign In") { Task { await add(p, oauth: false) } }
                .keyboardShortcut(.defaultAction)
                .disabled(working || email.isEmpty || password.isEmpty)
        }
    }

    /// Back to the provider list starts a clean form: nothing typed for one
    /// provider carries over to another.
    private func resetForm() {
        name = NSFullUserName()
        email = ""
        password = ""
        imapHost = ""
        imapPort = "993"
        smtpHost = ""
        smtpPort = "465"
        error = nil
    }

    private func help(_ p: Provider) -> String {
        switch p {
        case .gmail:
            return "Gmail needs an app password (Google Account ▸ Security ▸ 2-Step Verification ▸ App passwords). It is stored only in your Keychain."
        case .icloud:
            return "iCloud needs an app-specific password (account.apple.com ▸ Sign-In and Security ▸ App-Specific Passwords). It is stored only in your Keychain."
        case .imap:
            return "Laser Email only connects with encryption (TLS or STARTTLS)."
        }
    }

    private func add(_ p: Provider, oauth: Bool) async {
        working = true
        error = nil
        defer { working = false }
        var settings: AccountSettings?
        if p == .imap {
            let sp = Int(smtpPort) ?? 465
            settings = AccountSettings(kind: "imap",
                                       imap: Server(host: imapHost, port: Int(imapPort) ?? 993, security: "tls"),
                                       smtp: Server(host: smtpHost, port: sp, security: sp == 465 ? "tls" : "starttls"))
        }
        do {
            _ = try await model.client.addAccount(NewAccount(email: email, name: name, kind: p.rawValue, password: password, oauth: oauth, settings: settings))
            await model.refreshAll()
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

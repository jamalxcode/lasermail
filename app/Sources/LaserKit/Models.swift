import Foundation

// Wire types: they mirror the JSON laserd sends (snake_case keys, decoded
// with .convertFromSnakeCase). Arrays the Go side may send as null decode
// as empty.

public struct Address: Codable, Hashable, Sendable {
    public var name: String
    public var email: String

    public init(name: String = "", email: String) {
        self.name = name
        self.email = email
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        email = try c.decodeIfPresent(String.self, forKey: .email) ?? ""
    }

    /// What a list shows: the name, else the address.
    public var display: String { name.isEmpty ? email : name }
}

public struct Server: Codable, Hashable, Sendable {
    public var host: String
    public var port: Int
    public var security: String
    public init(host: String, port: Int, security: String) {
        self.host = host
        self.port = port
        self.security = security
    }
}

public struct AccountSettings: Codable, Hashable, Sendable {
    public var kind: String
    public var imap: Server
    public var smtp: Server
    public var savesSent: Bool?
    public var helpUrl: String?
    public init(kind: String, imap: Server, smtp: Server) {
        self.kind = kind
        self.imap = imap
        self.smtp = smtp
    }
}

public struct Account: Codable, Hashable, Identifiable, Sendable {
    public var id: Int64
    public var email: String
    public var name: String
    public var label: String
    public var auth: String
    public var settings: AccountSettings
    public var color: String?

    public var title: String { label.isEmpty ? email : label }
}

public struct AccountStatus: Codable, Hashable, Sendable {
    public var state: String
    public var error: String?
    public var lastSync: Date?
}

public struct AccountsReply: Decodable, Sendable {
    public var accounts: [Account]
    public var status: [String: AccountStatus]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accounts = try c.decodeIfPresent([Account].self, forKey: .accounts) ?? []
        status = try c.decodeIfPresent([String: AccountStatus].self, forKey: .status) ?? [:]
    }
    enum CodingKeys: String, CodingKey { case accounts, status }
}

public struct Mailbox: Codable, Hashable, Identifiable, Sendable {
    public var id: Int64
    public var accountId: Int64
    public var path: String
    public var name: String
    public var role: String
    public var unread: Int
    public var total: Int
}

public enum Role: String, CaseIterable, Sendable {
    case inbox, drafts, sent, junk, trash, archive, all, flagged
}

public struct Message: Codable, Hashable, Identifiable, Sendable {
    public var id: Int64
    public var accountId: Int64
    public var mailboxId: Int64
    public var uid: UInt32
    public var messageId: String
    public var inReplyTo: String
    public var threadKey: String
    public var subject: String
    public var from: Address
    public var to: [Address]
    public var cc: [Address]
    public var date: Date
    public var seen: Bool
    public var flagged: Bool
    public var answered: Bool
    public var hasAttachments: Bool
    public var size: Int64
    public var snippet: String
    public var hasBody: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int64.self, forKey: .id)
        accountId = try c.decode(Int64.self, forKey: .accountId)
        mailboxId = try c.decode(Int64.self, forKey: .mailboxId)
        uid = try c.decodeIfPresent(UInt32.self, forKey: .uid) ?? 0
        messageId = try c.decodeIfPresent(String.self, forKey: .messageId) ?? ""
        inReplyTo = try c.decodeIfPresent(String.self, forKey: .inReplyTo) ?? ""
        threadKey = try c.decodeIfPresent(String.self, forKey: .threadKey) ?? ""
        subject = try c.decodeIfPresent(String.self, forKey: .subject) ?? ""
        from = try c.decodeIfPresent(Address.self, forKey: .from) ?? Address(email: "")
        to = try c.decodeIfPresent([Address].self, forKey: .to) ?? []
        cc = try c.decodeIfPresent([Address].self, forKey: .cc) ?? []
        date = try c.decodeIfPresent(Date.self, forKey: .date) ?? .distantPast
        seen = try c.decodeIfPresent(Bool.self, forKey: .seen) ?? false
        flagged = try c.decodeIfPresent(Bool.self, forKey: .flagged) ?? false
        answered = try c.decodeIfPresent(Bool.self, forKey: .answered) ?? false
        hasAttachments = try c.decodeIfPresent(Bool.self, forKey: .hasAttachments) ?? false
        size = try c.decodeIfPresent(Int64.self, forKey: .size) ?? 0
        snippet = try c.decodeIfPresent(String.self, forKey: .snippet) ?? ""
        hasBody = try c.decodeIfPresent(Bool.self, forKey: .hasBody) ?? false
    }

    public var displaySubject: String { subject.isEmpty ? "(No Subject)" : subject }
}

public struct PrivacyReport: Codable, Hashable, Sendable {
    public var trackers: [String]
    public var remoteBlocked: Int
    public var linksCleaned: Int
    public var removed: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        trackers = try c.decodeIfPresent([String].self, forKey: .trackers) ?? []
        remoteBlocked = try c.decodeIfPresent(Int.self, forKey: .remoteBlocked) ?? 0
        linksCleaned = try c.decodeIfPresent(Int.self, forKey: .linksCleaned) ?? 0
        removed = try c.decodeIfPresent(Int.self, forKey: .removed) ?? 0
    }

    /// Anything worth a banner.
    public var isEmpty: Bool { trackers.isEmpty && remoteBlocked == 0 }
}

public struct Attachment: Codable, Hashable, Identifiable, Sendable {
    public var index: Int
    public var name: String
    public var type: String
    public var size: Int
    public var contentId: String?
    public var inline: Bool
    public var id: Int { index }
}

/// An opened message: the summary plus the sanitized body.
public struct MessageView: Decodable, Sendable {
    public var message: Message
    public var mailbox: String
    public var account: String
    public var replyTo: [Address]
    public var html: String
    public var privacy: PrivacyReport
    public var remoteImages: Bool
    public var attachments: [Attachment]
    public var listUnsubscribe: String?
    public var readReceiptRequested: Bool
    public var thread: [Message]

    enum CodingKeys: String, CodingKey {
        case mailbox, account, replyTo, html, privacy, remoteImages, attachments, listUnsubscribe, readReceiptRequested, thread
    }

    public init(from decoder: Decoder) throws {
        message = try Message(from: decoder) // Go flattens the embedded summary
        let c = try decoder.container(keyedBy: CodingKeys.self)
        mailbox = try c.decodeIfPresent(String.self, forKey: .mailbox) ?? ""
        account = try c.decodeIfPresent(String.self, forKey: .account) ?? ""
        replyTo = try c.decodeIfPresent([Address].self, forKey: .replyTo) ?? []
        html = try c.decodeIfPresent(String.self, forKey: .html) ?? ""
        privacy = try c.decode(PrivacyReport.self, forKey: .privacy)
        remoteImages = try c.decodeIfPresent(Bool.self, forKey: .remoteImages) ?? false
        attachments = try c.decodeIfPresent([Attachment].self, forKey: .attachments) ?? []
        listUnsubscribe = try c.decodeIfPresent(String.self, forKey: .listUnsubscribe)
        readReceiptRequested = try c.decodeIfPresent(Bool.self, forKey: .readReceiptRequested) ?? false
        thread = try c.decodeIfPresent([Message].self, forKey: .thread) ?? []
    }

    /// Files to list under the header (inline images are shown in the body).
    public var files: [Attachment] { attachments.filter { !$0.inline } }
}

public struct Draft: Codable, Hashable, Sendable {
    public var to: [Address]
    public var cc: [Address]
    public var bcc: [Address]
    public var subject: String
    public var text: String
    public var inReplyTo: String?
    public var references: [String]?

    public init(to: [Address] = [], cc: [Address] = [], bcc: [Address] = [], subject: String = "", text: String = "") {
        self.to = to
        self.cc = cc
        self.bcc = bcc
        self.subject = subject
        self.text = text
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        to = try c.decodeIfPresent([Address].self, forKey: .to) ?? []
        cc = try c.decodeIfPresent([Address].self, forKey: .cc) ?? []
        bcc = try c.decodeIfPresent([Address].self, forKey: .bcc) ?? []
        subject = try c.decodeIfPresent(String.self, forKey: .subject) ?? ""
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        inReplyTo = try c.decodeIfPresent(String.self, forKey: .inReplyTo)
        references = try c.decodeIfPresent([String].self, forKey: .references)
    }

    /// Parses "Ann <a@b.c>, d@e.f" as typed in a To field.
    public static func parseAddresses(_ s: String) -> [Address] {
        s.split(whereSeparator: { $0 == "," || $0 == ";" }).compactMap { part in
            let t = part.trimmingCharacters(in: .whitespaces)
            if t.isEmpty { return nil }
            if let lt = t.lastIndex(of: "<"), let gt = t.lastIndex(of: ">"), lt < gt {
                let name = t[..<lt].trimmingCharacters(in: CharacterSet(charactersIn: " \""))
                return Address(name: name, email: String(t[t.index(after: lt)..<gt]))
            }
            return Address(email: t)
        }
    }

    public static func formatAddresses(_ a: [Address]) -> String {
        a.map { $0.name.isEmpty ? $0.email : "\($0.name) <\($0.email)>" }.joined(separator: ", ")
    }
}

public struct DraftReply: Decodable, Sendable {
    public var accountId: Int64
    public var draft: Draft
}

public struct Query: Encodable, Hashable, Sendable {
    public var accountId: Int64 = 0
    public var mailboxId: Int64 = 0
    public var role: String = ""
    public var unread = false
    public var flagged = false
    public var text = ""
    public var before: Date?
    public var limit = 200
    public init() {}
}

public struct Hello: Decodable, Sendable {
    public var `protocol`: Int
    public var version: String
    public var oauth: Bool
    public var presets: [String: AccountSettings]?
}

public struct NewAccount: Encodable, Sendable {
    public var email: String
    public var name: String
    public var kind: String
    public var password: String
    public var oauth: Bool
    public var settings: AccountSettings?
    public init(email: String, name: String, kind: String, password: String, oauth: Bool, settings: AccountSettings? = nil) {
        self.email = email
        self.name = name
        self.kind = kind
        self.password = password
        self.oauth = oauth
        self.settings = settings
    }
}

public struct DaemonEvent: Decodable, Sendable {
    public var type: String
    public var accountId: Int64?
    public var mailboxId: Int64?
    public var count: Int?
    public var error: String?
}

public struct InlinePart: Decodable, Sendable {
    public var type: String
    public var data: Data
}

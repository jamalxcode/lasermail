import Foundation

/// Mail-style list dates: a time today, "Yesterday", the weekday this week,
/// else a short date.
public func listDate(_ d: Date, now: Date = Date(), calendar: Calendar = .current, locale: Locale = .current) -> String {
    if calendar.isDate(d, inSameDayAs: now) {
        return d.formatted(Date.FormatStyle(date: .omitted, time: .shortened).locale(locale))
    }
    if let y = calendar.date(byAdding: .day, value: -1, to: now), calendar.isDate(d, inSameDayAs: y) {
        return "Yesterday"
    }
    if let days = calendar.dateComponents([.day], from: d, to: now).day, days < 7, d < now {
        return d.formatted(Date.FormatStyle().weekday(.wide).locale(locale))
    }
    return d.formatted(Date.FormatStyle(date: .numeric, time: .omitted).locale(locale))
}

/// Initials for the sender's avatar (no pictures are ever fetched).
public func initials(_ a: Address) -> String {
    let words = a.display.split(whereSeparator: { $0 == " " || $0 == "." || $0 == "_" || $0 == "@" })
        .filter { $0.first?.isLetter == true }
    let letters = words.prefix(2).compactMap { $0.first }.map { String($0).uppercased() }
    return letters.isEmpty ? "?" : letters.joined()
}

/// A stable hue (0…1) per address, for avatar colors.
public func hue(for email: String) -> Double {
    var h: UInt32 = 2166136261
    for b in email.lowercased().utf8 { h = (h ^ UInt32(b)) &* 16777619 }
    return Double(h % 360) / 360
}

public func byteCount(_ n: Int) -> String {
    ByteCountFormatter.string(fromByteCount: Int64(n), countStyle: .file)
}

/// The sentence on the privacy banner, or nil when nothing was blocked.
public func privacySummary(_ r: PrivacyReport, remoteLoaded: Bool) -> String? {
    var parts: [String] = []
    let t = r.trackers.count
    if t > 0 { parts.append(t == 1 ? "blocked 1 tracker" : "blocked \(t) trackers") }
    if r.remoteBlocked > 0 && !remoteLoaded {
        parts.append(r.remoteBlocked == 1 ? "hid 1 remote image" : "hid \(r.remoteBlocked) remote images")
    }
    if r.linksCleaned > 0 { parts.append(r.linksCleaned == 1 ? "cleaned 1 link" : "cleaned \(r.linksCleaned) links") }
    guard !parts.isEmpty else { return nil }
    var s = "Laser " + parts.joined(separator: ", ")
    if let last = s.range(of: ", ", options: .backwards), parts.count > 1 {
        s.replaceSubrange(last, with: " and ")
    }
    return s + "."
}

import AppKit
import LaserKit
import SwiftUI
import WebKit

/// Shows a sanitized message. Defense in depth on top of laserd's filter and
/// the CSP in the page: JavaScript is off, nothing is stored (non-persistent
/// data store), a WKContentRuleList blocks every remote load (or, after
/// "Load Images", everything but https images), links open in the browser,
/// and inline cid: images are served from the local message.
struct MailWebView: NSViewRepresentable {
    let html: String
    let remoteImages: Bool
    let messageID: Int64
    let client: DaemonClient

    func makeCoordinator() -> Coordinator { Coordinator(client: client) }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        config.mediaTypesRequiringUserActionForPlayback = .all
        config.setURLSchemeHandler(context.coordinator, forURLScheme: "cid")
        let wv = WKWebView(frame: .zero, configuration: config)
        wv.navigationDelegate = context.coordinator
        wv.allowsLinkPreview = false
        wv.appearance = NSAppearance(named: .aqua) // mail is designed on white, as in Mail
        return wv
    }

    func updateNSView(_ wv: WKWebView, context: Context) {
        let key = "\(messageID)-\(remoteImages)-\(html.hashValue)"
        guard context.coordinator.loadedKey != key else { return }
        context.coordinator.loadedKey = key
        context.coordinator.messageID = messageID
        let html = html
        BlockRules.list(remote: remoteImages) { list in
            let ucc = wv.configuration.userContentController
            ucc.removeAllContentRuleLists()
            if let list { ucc.add(list) } else { return } // never show mail without the rules
            wv.loadHTMLString(html, baseURL: nil)
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKURLSchemeHandler {
        let client: DaemonClient
        var loadedKey = ""
        var messageID: Int64 = 0
        private var stopped = Set<ObjectIdentifier>()
        private let lock = NSLock()

        init(client: DaemonClient) { self.client = client }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url else { return decisionHandler(.cancel) }
            if url.absoluteString == "about:blank" { return decisionHandler(.allow) }
            if action.navigationType == .linkActivated, let s = url.scheme?.lowercased(), ["http", "https", "mailto", "tel"].contains(s) {
                NSWorkspace.shared.open(url)
            } else if url.scheme == nil, url.fragment != nil {
                return decisionHandler(.allow) // in-page anchors
            }
            decisionHandler(.cancel)
        }

        func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
            guard let url = task.request.url else { return }
            let cid = (url.absoluteString.removingPercentEncoding ?? url.absoluteString).replacingOccurrences(of: "cid:", with: "")
            let id = messageID
            let key = ObjectIdentifier(task)
            Task {
                let part = try? await client.inlinePart(id, contentId: cid)
                await MainActor.run {
                    self.lock.lock()
                    let gone = self.stopped.remove(key) != nil
                    self.lock.unlock()
                    if gone { return }
                    guard let part else {
                        task.didFailWithError(NSError(domain: "LaserEmail", code: 404))
                        return
                    }
                    task.didReceive(URLResponse(url: url, mimeType: part.type, expectedContentLength: part.data.count, textEncodingName: nil))
                    task.didReceive(part.data)
                    task.didFinish()
                }
            }
        }

        func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {
            lock.lock()
            stopped.insert(ObjectIdentifier(task))
            lock.unlock()
        }
    }
}

/// Compiled once, reused for every message.
enum BlockRules {
    // Everything remote is blocked.
    static let blockAll = """
    [{"trigger":{"url-filter":"^[a-z][a-z0-9+.-]*://"},"action":{"type":"block"}},
     {"trigger":{"url-filter":".*","resource-type":["ping","popup","script","media","raw"]},"action":{"type":"block"}}]
    """
    // After "Load Images": https images only (the CSP allows nothing else either).
    static let imagesOnly = """
    [{"trigger":{"url-filter":"^[a-z][a-z0-9+.-]*://"},"action":{"type":"block"}},
     {"trigger":{"url-filter":"^https://","resource-type":["image"]},"action":{"type":"ignore-previous-rules"}},
     {"trigger":{"url-filter":".*","resource-type":["ping","popup","script","media","raw"]},"action":{"type":"block"}}]
    """

    @MainActor private static var cache: [Bool: WKContentRuleList] = [:]

    @MainActor static func list(remote: Bool, _ done: @escaping (WKContentRuleList?) -> Void) {
        if let l = cache[remote] { return done(l) }
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: remote ? "laser-images" : "laser-block-all",
                                                                encodedContentRuleList: remote ? imagesOnly : blockAll) { list, error in
            if let error { NSLog("content rules: \(error)") }
            if let list { cache[remote] = list }
            done(list)
        }
    }
}

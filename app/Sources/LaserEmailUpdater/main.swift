import Foundation
import LaserKit

// Laser Email Updater: the one part of Laser Email allowed on the network
// besides the mail connections, modelled on laserdot-go's updater helper. It
// runs only when the user chooses Check for Updates…, talks only to GitHub
// (github.com and the hosts GitHub redirects release downloads to), sends no
// credentials and nothing about the Mac, writes UpdateStatus and exits.
// The app then checks the download offline (ReleaseProof, bundle id, version)
// before installing anything.
//
// Usage: LaserEmailUpdater --app <Laser Email.app> [--status <file>]

let repo = Updater.repo
let allowedHosts: Set<String> = ["github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"]

/// Follows redirects only to GitHub's own hosts, over HTTPS.
final class RedirectGuard: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        guard let u = request.url, u.scheme == "https", let h = u.host, allowedHosts.contains(h) else {
            completionHandler(nil)
            return
        }
        completionHandler(request)
    }
}

/// Refuses every redirect, so the answer's Location header can be read.
final class NoRedirects: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

func ephemeral() -> URLSessionConfiguration {
    let c = URLSessionConfiguration.ephemeral // no cookies, no cache
    c.timeoutIntervalForRequest = 60
    c.timeoutIntervalForResource = 30 * 60
    c.httpAdditionalHeaders = ["User-Agent": "LaserEmail-Updater"]
    return c
}

let session = URLSession(configuration: ephemeral(), delegate: RedirectGuard(), delegateQueue: nil)
let probe = URLSession(configuration: ephemeral(), delegate: NoRedirects(), delegateQueue: nil)

struct HTTPError: Error, LocalizedError {
    let status: Int
    var errorDescription: String? {
        switch status {
        case 0: return "Could not reach GitHub. Check your internet connection."
        case 403, 429: return "GitHub is limiting requests from your network right now. Try again later."
        default: return "GitHub answered HTTP \(status)."
        }
    }
}

/// The newest release's version, from where github.com/<repo>/releases/latest
/// redirects (…/releases/tag/v0.1.4). The ordinary web page, so the API's
/// limit for anonymous clients doesn't apply.
func latestVersion() throws -> String {
    let url = URL(string: "https://github.com/\(repo)/releases/latest")!
    let done = DispatchSemaphore(value: 0)
    var result: Result<String, Error> = .failure(HTTPError(status: 0))
    probe.dataTask(with: url) { _, response, error in
        defer { done.signal() }
        if let error { result = .failure(error); return }
        let http = response as? HTTPURLResponse
        let status = http?.statusCode ?? 0
        guard (300..<400).contains(status), let loc = http?.value(forHTTPHeaderField: "Location"),
              let target = URL(string: loc, relativeTo: url), target.host == "github.com",
              target.path.hasPrefix("/\(repo)/releases/tag/") else {
            result = .failure(HTTPError(status: status))
            return
        }
        result = .success(target.lastPathComponent)
    }.resume()
    done.wait()
    let tag = try result.get()
    return tag.hasPrefix("v") ? String(tag.dropFirst()) : tag
}

/// Downloads to `dest`; moved into place only once complete. A broken-off
/// download is tried again (three times); GitHub's refusals are not.
func download(_ url: URL, to dest: URL) throws {
    var attempt = 0
    while true {
        attempt += 1
        let done = DispatchSemaphore(value: 0)
        var failure: Error?
        session.downloadTask(with: url) { tmp, response, error in
            defer { done.signal() }
            if let error { failure = error; return }
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            guard (200..<300).contains(status), let tmp else { failure = HTTPError(status: status); return }
            do {
                try? FileManager.default.removeItem(at: dest)
                try FileManager.default.moveItem(at: tmp, to: dest)
            } catch { failure = error }
        }.resume()
        done.wait()
        guard let failure else { return }
        if failure is URLError, attempt < 3 {
            Thread.sleep(forTimeInterval: Double(attempt) * 3)
            continue
        }
        throw failure
    }
}

let args = CommandLine.arguments
func arg(_ name: String) -> String? {
    guard let i = args.firstIndex(of: name), i + 1 < args.count else { return nil }
    return args[i + 1]
}
let appURL = arg("--app").map { URL(fileURLWithPath: $0) }
    ?? Bundle.main.bundleURL.deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
let statusURL = arg("--status").map { URL(fileURLWithPath: $0) } ?? Updater.statusFile

func finish(_ s: UpdateStatus) -> Never {
    try? FileManager.default.createDirectory(at: statusURL.deletingLastPathComponent(), withIntermediateDirectories: true)
    try? s.save(to: statusURL)
    exit(s.state == .failed ? 1 : 0)
}

guard let current = AppVersion(bundle: appURL) else {
    finish(UpdateStatus(state: .failed, message: "Laser Email.app not found at \(appURL.path)"))
}

do {
    let latest = try latestVersion()
    guard !latest.isEmpty, latest.allSatisfy({ $0.isNumber || $0 == "." }) else {
        finish(UpdateStatus(state: .failed, message: "GitHub named an unexpected release: \(latest)"))
    }
    guard current < AppVersion(latest) else { finish(UpdateStatus(state: .upToDate, latest: latest)) }
    // The release workflow names the disk image LaserEmail-<version>.dmg and
    // publishes its build proof next to it.
    let name = "LaserEmail-\(latest).dmg"
    let base = "https://github.com/\(repo)/releases/download/v\(latest)/"
    let folder = statusURL.deletingLastPathComponent()
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    let dest = folder.appendingPathComponent(name)
    let proof = folder.appendingPathComponent(name + ".sigstore.json")
    try? FileManager.default.removeItem(at: proof)
    do {
        try download(URL(string: base + name + ".sigstore.json")!, to: proof)
    } catch let e as HTTPError where e.status == 404 {
        // No proof published (releases before 0.1.4): the app refuses to install it.
    }
    try? FileManager.default.removeItem(at: dest) // always fetch afresh
    try download(URL(string: base + name)!, to: dest)
    finish(UpdateStatus(state: .downloaded, latest: latest, file: dest.path))
} catch {
    finish(UpdateStatus(state: .failed, message: (error as? LocalizedError)?.errorDescription ?? error.localizedDescription))
}

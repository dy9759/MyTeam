import AppKit
import Foundation
import Security

enum NativeError: Error {
    case invalidArguments(String)
    case keychain(OSStatus)
    case bookmarkStore(String)
}

struct TokenResponse: Encodable {
    let token: String?
}

struct PathsResponse: Encodable {
    let paths: [String]
}

struct EmptyResponse: Encodable {}

let keychainService = "ai.myteam.desktop"
let keychainAccount = "session-token"
let bookmarkDefaultsKey = "desktop.bookmarks"

func printJSON<T: Encodable>(_ value: T) throws {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let data = try encoder.encode(value)
    if let json = String(data: data, encoding: .utf8) {
        FileHandle.standardOutput.write(Data(json.utf8))
    }
}

func keychainGet() throws -> String? {
    let query: [String: Any] = [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: keychainService,
        kSecAttrAccount as String: keychainAccount,
        kSecReturnData as String: true,
        kSecMatchLimit as String: kSecMatchLimitOne
    ]

    var item: CFTypeRef?
    let status = SecItemCopyMatching(query as CFDictionary, &item)
    if status == errSecItemNotFound {
        return nil
    }
    guard status == errSecSuccess else {
        throw NativeError.keychain(status)
    }
    guard let data = item as? Data else {
        return nil
    }
    return String(data: data, encoding: .utf8)
}

func keychainSet(token: String) throws {
    let encoded = Data(token.utf8)
    let query: [String: Any] = [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: keychainService,
        kSecAttrAccount as String: keychainAccount,
    ]
    let attributes: [String: Any] = [
        kSecValueData as String: encoded,
    ]

    let updateStatus = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
    if updateStatus == errSecItemNotFound {
        let insertQuery: [String: Any] = query.merging(attributes) { _, new in new }
        let addStatus = SecItemAdd(insertQuery as CFDictionary, nil)
        guard addStatus == errSecSuccess else {
            throw NativeError.keychain(addStatus)
        }
        return
    }

    guard updateStatus == errSecSuccess else {
        throw NativeError.keychain(updateStatus)
    }
}

func keychainDelete() throws {
    let query: [String: Any] = [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: keychainService,
        kSecAttrAccount as String: keychainAccount,
    ]
    let status = SecItemDelete(query as CFDictionary)
    guard status == errSecSuccess || status == errSecItemNotFound else {
        throw NativeError.keychain(status)
    }
}

func showNotification(title: String, body: String) throws {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
    process.arguments = [
        "-e",
        "display notification \"\(body.replacingOccurrences(of: "\"", with: "\\\""))\" with title \"\(title.replacingOccurrences(of: "\"", with: "\\\""))\""
    ]
    try process.run()
    process.waitUntilExit()
}

func openPath(_ path: String) {
    NSWorkspace.shared.open(URL(fileURLWithPath: path))
}

func revealPath(_ path: String) {
    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
}

func openPanel() -> [String] {
    let panel = NSOpenPanel()
    panel.allowsMultipleSelection = true
    panel.canChooseDirectories = true
    panel.canChooseFiles = true
    panel.resolvesAliases = true
    let response = panel.runModal()
    guard response == .OK else {
        return []
    }
    return panel.urls.map(\.path)
}

func loadBookmarks() -> [String: String] {
    UserDefaults.standard.dictionary(forKey: bookmarkDefaultsKey) as? [String: String] ?? [:]
}

func storeBookmark(path: String) throws {
    let url = URL(fileURLWithPath: path)
    let data = try url.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
    var bookmarks = loadBookmarks()
    bookmarks[path] = data.base64EncodedString()
    UserDefaults.standard.set(bookmarks, forKey: bookmarkDefaultsKey)
}

func resolveBookmark(path: String) throws -> String {
    let bookmarks = loadBookmarks()
    guard let encoded = bookmarks[path], let data = Data(base64Encoded: encoded) else {
        throw NativeError.bookmarkStore("Bookmark not found for path \(path)")
    }
    var stale = false
    let url = try URL(resolvingBookmarkData: data, options: [.withSecurityScope], relativeTo: nil, bookmarkDataIsStale: &stale)
    return url.path
}

let arguments = Array(CommandLine.arguments.dropFirst())

do {
    guard let command = arguments.first else {
        throw NativeError.invalidArguments("Missing command")
    }

    switch command {
    case "keychain.get":
        try printJSON(TokenResponse(token: try keychainGet()))
    case "keychain.set":
        guard arguments.count >= 2 else {
            throw NativeError.invalidArguments("Missing token")
        }
        try keychainSet(token: arguments[1])
        try printJSON(EmptyResponse())
    case "keychain.delete":
        try keychainDelete()
        try printJSON(EmptyResponse())
    case "notification.show":
        guard arguments.count >= 3 else {
          throw NativeError.invalidArguments("Usage: notification.show <title> <body>")
        }
        try showNotification(title: arguments[1], body: arguments[2])
        try printJSON(EmptyResponse())
    case "file.open":
        guard arguments.count >= 2 else {
            throw NativeError.invalidArguments("Missing path")
        }
        openPath(arguments[1])
        try printJSON(EmptyResponse())
    case "file.reveal":
        guard arguments.count >= 2 else {
            throw NativeError.invalidArguments("Missing path")
        }
        revealPath(arguments[1])
        try printJSON(EmptyResponse())
    case "file.openPanel":
        try printJSON(PathsResponse(paths: openPanel()))
    case "bookmark.store":
        guard arguments.count >= 2 else {
            throw NativeError.invalidArguments("Missing path")
        }
        try storeBookmark(path: arguments[1])
        try printJSON(EmptyResponse())
    case "bookmark.resolve":
        guard arguments.count >= 2 else {
            throw NativeError.invalidArguments("Missing path")
        }
        try printJSON(PathsResponse(paths: [try resolveBookmark(path: arguments[1])]))
    default:
        throw NativeError.invalidArguments("Unknown command: \(command)")
    }
} catch {
    let message = String(describing: error)
    FileHandle.standardError.write(Data(message.utf8))
    exit(1)
}

import AppKit
import CryptoKit
import Foundation
import UniformTypeIdentifiers

let MAX_BYTES = 64 * 1024 * 1024

func die(_ m: String) -> Never {
    FileHandle.standardError.write(("clipsync: " + m + "\n").data(using: .utf8)!)
    exit(1)
}
func note(_ m: String) {
    FileHandle.standardError.write(("clipsync: " + m + "\n").data(using: .utf8)!)
}

let staging = FileManager.default.homeDirectoryForCurrentUser
    .appendingPathComponent(".clipsync/incoming")

/// Динамические UTI (dyn.ah62d…) кодируют исходное имя типа. Разворачиваем обратно,
/// чтобы узнавать типы по-человечески, а не по захардкоженной абракадабре.
func pbTypeName(_ uti: String) -> String? {
    guard uti.hasPrefix("dyn."), let t = UTType(uti) else { return nil }
    for (k, v) in t.tags where k.rawValue == "com.apple.nspboard-type" {
        return v.first
    }
    return nil
}

let T_FILE_URL = "public.file-url"
let T_FILENAMES = "NSFilenamesPboardType"
let N_APPLE_URL = "Apple URL pasteboard type"
let N_GNOME = "x-special/gnome-copied-files"

func packPath(_ url: URL) throws -> (Data, Bool) {
    var isDir: ObjCBool = false
    guard FileManager.default.fileExists(atPath: url.path, isDirectory: &isDir) else {
        throw NSError(domain: "clipsync", code: 2,
                      userInfo: [NSLocalizedDescriptionKey: "нет файла"])
    }
    if !isDir.boolValue { return (try Data(contentsOf: url), false) }
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
    p.arguments = ["-czf", "-", "-C", url.deletingLastPathComponent().path, url.lastPathComponent]
    let pipe = Pipe()
    p.standardOutput = pipe
    try p.run()
    let d = pipe.fileHandleForReading.readDataToEndOfFile()
    p.waitUntilExit()
    return (d, true)
}

func unpack(_ data: Data, name: String, isDir: Bool, into dir: URL) throws -> URL {
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true,
                                            attributes: [.posixPermissions: 0o700])
    if !isDir {
        let dest = dir.appendingPathComponent(name)
        try data.write(to: dest)
        return dest
    }
    let tmp = dir.appendingPathComponent("_ar.tgz")
    try data.write(to: tmp)
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
    p.arguments = ["-xzf", tmp.path, "-C", dir.path]
    try p.run()
    p.waitUntilExit()
    try? FileManager.default.removeItem(at: tmp)
    return dir.appendingPathComponent(name)
}

/// Принесённые копии накапливаются — подчищаем всё, что старше недели.
func pruneStaging(olderThanDays days: Int) {
    let fm = FileManager.default
    guard let kids = try? fm.contentsOfDirectory(at: staging, includingPropertiesForKeys: [.contentModificationDateKey]) else { return }
    let cutoff = Date().addingTimeInterval(-Double(days) * 86400)
    for k in kids {
        let d = (try? k.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
        if let d = d, d < cutoff { try? fm.removeItem(at: k) }
    }
}

func doExport() {
    let pb = NSPasteboard.general
    guard let items = pb.pasteboardItems, !items.isEmpty else { die("буфер пуст") }

    var outItems: [[String: Data]] = []
    var outFiles: [String: [String: Any]] = [:]
    var total = 0
    var droppedJVM = 0

    for item in items {
        var m: [String: Data] = [:]
        for t in item.types {
            let raw = t.rawValue
            // обещанные файлы данных не несут — их отдаёт приложение-источник по запросу
            if raw.hasPrefix("com.apple.pasteboard.promised") { continue }
            // ссылка на объект внутри живой JVM исходной IDE — на другой машине пустой указатель
            if let n = pbTypeName(raw), n.contains("x-java-jvm-local-objectref") {
                droppedJVM += 1
                continue
            }
            guard let d = item.data(forType: t) else { continue }
            m[raw] = d
            total += d.count
        }
        if m.isEmpty { continue }
        outItems.append(m)

        if let d = item.data(forType: NSPasteboard.PasteboardType(T_FILE_URL)),
           let s = String(data: d, encoding: .utf8),
           let u = URL(string: s), u.isFileURL {
            do {
                let (data, isDir) = try packPath(u)
                total += data.count
                outFiles[String(outItems.count - 1)] = [
                    "name": u.lastPathComponent, "isDir": isDir, "data": data,
                ]
            } catch {
                note("пропущен \(u.lastPathComponent): \(error.localizedDescription)")
            }
        }
    }

    if outItems.isEmpty { die("нечего переносить") }
    if total > MAX_BYTES {
        die("слишком много: \(total / 1024 / 1024) МБ при лимите \(MAX_BYTES / 1024 / 1024) МБ")
    }

    let payload: [String: Any] = ["version": 1, "items": outItems, "files": outFiles]
    guard let plist = try? PropertyListSerialization.data(fromPropertyList: payload,
                                                          format: .binary, options: 0)
    else { die("не сериализовать") }
    FileHandle.standardOutput.write(plist)
    var msg = "\(outItems.count) эл."
    if !outFiles.isEmpty { msg += ", \(outFiles.count) файл(ов)" }
    if droppedJVM > 0 { msg += ", отброшено \(droppedJVM) jvm-ссылок" }
    note(msg + ", \(total / 1024) КБ")
}

func doImport() {
    let raw = FileHandle.standardInput.readDataToEndOfFile()
    guard !raw.isEmpty else { die("на входе пусто") }
    guard let obj = try? PropertyListSerialization.propertyList(from: raw, options: [], format: nil),
          let payload = obj as? [String: Any],
          var items = payload["items"] as? [[String: Data]]
    else { die("не разобрать полезную нагрузку") }
    let files = payload["files"] as? [String: [String: Any]] ?? [:]

    if !files.isEmpty {
        pruneStaging(olderThanDays: 7)
        let dir = staging.appendingPathComponent(
            String(Int(Date().timeIntervalSince1970)) + "-" + UUID().uuidString.prefix(6))
        var newPaths: [String] = []
        var newURLs: [String] = []

        // порядок словаря не определён — идём строго по возрастанию индекса элемента
        for key in files.keys.sorted(by: { (Int($0) ?? 0) < (Int($1) ?? 0) }) {
            guard let meta = files[key], let i = Int(key), i < items.count,
                  let data = meta["data"] as? Data, let name = meta["name"] as? String
            else { continue }
            let isDir = (meta["isDir"] as? Bool) ?? false
            do {
                let dest = try unpack(data, name: name, isDir: isDir,
                                      into: dir.appendingPathComponent(key))
                items[i][T_FILE_URL] = dest.absoluteString.data(using: .utf8)
                items[i]["public.utf8-plain-text"] = dest.path.data(using: .utf8)
                newPaths.append(dest.path)
                newURLs.append(dest.absoluteString)
            } catch {
                note("не распаковать \(name): \(error.localizedDescription)")
            }
        }

        // остальные типы, несущие абсолютные пути, тоже переписываем на локальные копии
        for i in items.indices {
            for (uti, _) in items[i] {
                guard let n = pbTypeName(uti) else { continue }
                if n == N_APPLE_URL {
                    var s = newURLs.joined(separator: "\r\n")
                    s += "\0"
                    items[i][uti] = s.data(using: .utf8)
                } else if n == N_GNOME {
                    items[i][uti] = ("copy\n" + newURLs.joined(separator: "\n")).data(using: .utf8)
                }
            }
        }
    }

    let pb = NSPasteboard.general
    pb.clearContents()
    var written: [NSPasteboardItem] = []
    for m in items {
        let it = NSPasteboardItem()
        for (k, v) in m {
            if !it.setData(v, forType: NSPasteboard.PasteboardType(k)) {
                note("не принят тип \(k) (\(v.count) б)")
            }
        }
        written.append(it)
    }
    guard pb.writeObjects(written) else { die("не записать в буфер") }

    // writeObjects отдаёт данные пастборд-серверу асинхронно, и изнутри пишущего
    // процесса это не проверить: pasteboardItems вернёт наши же элементы. Если выйти
    // сразу, часть типов (в первую очередь public.file-url) не успевает доехать,
    // поэтому просто держимся живыми, как это делает сам pbcopy.
    Thread.sleep(forTimeInterval: 0.3)

    var msg = "восстановлено \(written.count) эл."
    if !files.isEmpty { msg += ", \(files.count) файл(ов)" }
    note(msg)
}

/// Отпечаток содержимого буфера: по нему двойное копирование отличается от двух
/// разных копирований подряд. Берём первый «устойчивый» тип — текст, путь
/// файла или картинку; служебные типы приложений меняются от копии к копии.
func fingerprint(_ pb: NSPasteboard) -> String {
    let preferred = ["public.utf8-plain-text", T_FILE_URL, "public.png", "public.tiff", "public.rtf"]
    var h = SHA256()
    for item in pb.pasteboardItems ?? [] {
        let types = item.types.map { $0.rawValue }
        guard let t = preferred.first(where: { types.contains($0) }) ?? types.sorted().first,
              let d = item.data(forType: NSPasteboard.PasteboardType(t)) else { continue }
        h.update(data: t.data(using: .utf8)!)
        h.update(data: d)
    }
    return h.finalize().map { String(format: "%02x", $0) }.joined()
}

/// Следит за буфером и пишет в stdout строку "double", когда одно и то же
/// скопировано дважды подряд за `window` секунд (два нажатия ⌘C).
/// Повторный ⌘C содержимое не меняет — меняется только changeCount, поэтому
/// следим за ним, а содержимое сравниваем по отпечатку.
func doWatch(window: Double) {
    setvbuf(stdout, nil, _IOLBF, 0)
    // CLIPSYNC_PASTEBOARD=find следит за буфером поиска — для тестов, чтобы
    // не трогать настоящий буфер обмена.
    let pb = ProcessInfo.processInfo.environment["CLIPSYNC_PASTEBOARD"] == "find"
        ? NSPasteboard(name: .find) : NSPasteboard.general
    var last = pb.changeCount
    var lastFP = ""
    var lastAt = Date.distantPast
    note("слежу за буфером: двойное копирование в пределах \(window) с")
    while true {
        Thread.sleep(forTimeInterval: 0.08)
        let c = pb.changeCount
        if c == last { continue }
        let now = Date()
        let fp = fingerprint(pb)
        let twoAtOnce = c - last >= 2 // оба ⌘C успели между опросами
        if twoAtOnce || (fp == lastFP && now.timeIntervalSince(lastAt) <= window) {
            print("double")
            lastFP = ""
            lastAt = .distantPast
        } else {
            lastFP = fp
            lastAt = now
        }
        last = c
    }
}

switch CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "" {
case "export": doExport()
case "import": doImport()
case "watch":
    let w = CommandLine.arguments.count > 2 ? Double(CommandLine.arguments[2]) ?? 1.0 : 1.0
    doWatch(window: w)
default: die("использование: clipsync export | clipsync import | clipsync watch [окно_сек]")
}

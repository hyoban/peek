import AppKit

// trayicon.swift renders the menu bar icon, trayicon.png, from an SF
// Symbol:
//
//	swift internal/app/trayicon.swift arrow.triangle.branch internal/app/trayicon.png
//
// as AppKit draws it, black on transparent for macOS to tint, 18 points
// square at twice the pixels.
let args = CommandLine.arguments
let config = NSImage.SymbolConfiguration(pointSize: 15, weight: .regular)
guard let sym = NSImage(systemSymbolName: args[1], accessibilityDescription: nil)?.withSymbolConfiguration(config) else {
    fatalError("no symbol \(args[1])")
}
let px = 36
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px, bitsPerSample: 8, samplesPerPixel: 4,
                           hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
rep.size = NSSize(width: 18, height: 18)
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
let s = sym.size
let r = NSRect(x: (18 - s.width) / 2, y: (18 - s.height) / 2, width: s.width, height: s.height)
sym.draw(in: r)
NSColor.black.set()
r.fill(using: .sourceAtop)
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: args[2]))

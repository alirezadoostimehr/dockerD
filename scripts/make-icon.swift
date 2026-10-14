import AppKit
import Foundation

// Draw the app's own mark with native vector primitives at every icon size.
let destination = URL(fileURLWithPath: CommandLine.arguments[1])
try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: true)
for size in [16, 32, 128, 256, 512] {
  for scale in [1, 2] {
    let pixels = size * scale
    let image = NSImage(size: NSSize(width: pixels, height: pixels))
    image.lockFocus()
    let bounds = NSRect(
      x: Double(pixels) * 0.08, y: Double(pixels) * 0.08, width: Double(pixels) * 0.84,
      height: Double(pixels) * 0.84)
    let background = NSBezierPath(
      roundedRect: bounds, xRadius: Double(pixels) * 0.19, yRadius: Double(pixels) * 0.19)
    NSGradient(
      starting: NSColor(srgbRed: 0.16, green: 0.34, blue: 0.85, alpha: 1),
      ending: NSColor(srgbRed: 0.30, green: 0.63, blue: 1, alpha: 1))!.draw(
        in: background, angle: 65)
    if let symbol = NSImage(systemSymbolName: "shippingbox.fill", accessibilityDescription: nil)?
      .withSymbolConfiguration(.init(pointSize: Double(pixels) * 0.47, weight: .medium))
    {
      let tinted = NSImage(size: symbol.size)
      tinted.lockFocus()
      NSColor.white.set()
      NSRect(origin: .zero, size: symbol.size).fill()
      symbol.draw(at: .zero, from: .zero, operation: .destinationIn, fraction: 1)
      tinted.unlockFocus()
      tinted.draw(
        in: NSRect(
          x: Double(pixels) * 0.26, y: Double(pixels) * 0.25, width: Double(pixels) * 0.48,
          height: Double(pixels) * 0.49))
    }
    image.unlockFocus()
    let bitmap = NSBitmapImageRep(data: image.tiffRepresentation!)!
    let filename = "icon_\(size)x\(size)\(scale == 2 ? "@2x" : "").png"
    try bitmap.representation(using: .png, properties: [:])!.write(
      to: destination.appendingPathComponent(filename))
  }
}

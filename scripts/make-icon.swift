// make-icon.swift — 渲染 ResidentKit 应用图标（1024×1024 母版）。
//
//   swift scripts/make-icon.swift <out.png>             满版母版（iOS 用）
//   swift scripts/make-icon.swift <out.png> --rounded   圆角，角外透明
//                                                       （README / 网页用）
//
// 设计含义「常驻」：核心加一颗沿轨道运行的卫星，始终运行、永不停止。
// 只用黑白两种纯色，无渐变。iOS 会自行套圆角遮罩，所以画满版。

import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let S: CGFloat = 1024
let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "icon-1024.png"
let rounded = CommandLine.arguments.contains("--rounded")

let black = CGColor(srgbRed: 0.04, green: 0.04, blue: 0.05, alpha: 1)
let white = CGColor(srgbRed: 1, green: 1, blue: 1, alpha: 1)

let cs = CGColorSpace(name: CGColorSpace.sRGB)!
let ctx = CGContext(data: nil, width: Int(S), height: Int(S), bitsPerComponent: 8,
                    bytesPerRow: 0, space: cs,
                    bitmapInfo: (rounded ? CGImageAlphaInfo.premultipliedLast
                                         : CGImageAlphaInfo.noneSkipLast).rawValue)!
// 原点移到左上角，几何坐标更直观。
ctx.translateBy(x: 0, y: S)
ctx.scaleBy(x: 1, y: -1)

let c = CGPoint(x: S / 2, y: S / 2)
func pt(_ a: CGFloat, _ r: CGFloat) -> CGPoint { CGPoint(x: c.x + r * cos(a), y: c.y + r * sin(a)) }
func disc(_ p: CGPoint, _ r: CGFloat) { ctx.fillEllipse(in: CGRect(x: p.x - r, y: p.y - r, width: 2 * r, height: 2 * r)) }

// 网页版用 iOS 风格圆角（约为边长的 22.37%）。
if rounded {
    ctx.addPath(CGPath(roundedRect: CGRect(x: 0, y: 0, width: S, height: S),
                       cornerWidth: S * 0.2237, cornerHeight: S * 0.2237, transform: nil))
    ctx.clip()
}

// 1. 背景：纯黑。
ctx.setFillColor(black)
ctx.fill(CGRect(x: 0, y: 0, width: S, height: S))

ctx.setFillColor(white)
ctx.setStrokeColor(white)
ctx.setLineCap(.round)

// 2. 外轨道：在卫星两侧断开，营造运动感。
let R: CGFloat = 340
let satAngle: CGFloat = -.pi / 4              // 右上
let satR: CGFloat = 50
let gap: CGFloat = 0.30                       // 两侧各留的间隙（弧度）
ctx.setLineWidth(24)
ctx.addArc(center: c, radius: R, startAngle: satAngle + gap,
           endAngle: satAngle + 2 * .pi - gap, clockwise: false)
ctx.strokePath()

// 3. 卫星。
disc(pt(satAngle, R), satR)

// 4. 内轨道：细线，低调。
ctx.setLineWidth(8)
ctx.strokeEllipse(in: CGRect(x: c.x - 232, y: c.y - 232, width: 464, height: 464))

// 5. 核心。
disc(c, 128)

// 写出 PNG（iOS 母版不透明：iOS 图标不能带 alpha）。
let img = ctx.makeImage()!
let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: out) as CFURL,
                                           UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, img, nil)
guard CGImageDestinationFinalize(dest) else { fatalError("could not write \(out)") }
print("wrote \(out)")

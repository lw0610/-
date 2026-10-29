import Foundation
import Vision
import AppKit

let args = CommandLine.arguments
guard args.count > 1 else {
    print("usage: ocr.swift <imagePath>")
    exit(1)
}
let path = args[1]
guard let img = NSImage(contentsOfFile: path),
      let cg = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    print("ERROR: cannot load image")
    exit(1)
}

let request = VNRecognizeTextRequest { req, err in
    guard let obs = req.results as? [VNRecognizedTextObservation] else { return }
    for o in obs {
        if let top = o.topCandidates(1).first {
            // approximate row by y coordinate
            let bbox = o.boundingBox
            let y = bbox.origin.y
            let x = bbox.origin.x
            print(String(format: "%.3f\t%.3f\t%@", y, x, top.string))
        }
    }
}
request.recognitionLevel = .accurate
request.recognitionLanguages = ["zh-Hans", "en-US"]
request.usesLanguageCorrection = true

let handler = VNImageRequestHandler(cgImage: cg, options: [:])
try? handler.perform([request])

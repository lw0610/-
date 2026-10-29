import Foundation
import Vision
import AppKit

guard CommandLine.arguments.count >= 2 else {
    print("usage: ocr.swift <image_path>")
    exit(1)
}

let path = CommandLine.arguments[1]
guard let image = NSImage(contentsOfFile: path),
      let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    print("ERROR: cannot load image \(path)")
    exit(1)
}

let request = VNRecognizeTextRequest { request, error in
    if let error = error {
        print("ERROR: \(error)")
        return
    }
    guard let observations = request.results as? [VNRecognizedTextObservation] else { return }
    // Sort top-to-bottom, left-to-right
    let sorted = observations.sorted { a, b in
        let ay = a.boundingBox.midY
        let by = b.boundingBox.midY
        if abs(ay - by) > 0.01 {
            return ay > by
        }
        return a.boundingBox.minX < b.boundingBox.minX
    }
    for obs in sorted {
        if let top = obs.topCandidates(1).first {
            let bb = obs.boundingBox
            print(String(format: "[%.3f,%.3f,%.3f,%.3f] %@", bb.minX, bb.minY, bb.width, bb.height, top.string))
        }
    }
}

request.recognitionLevel = .accurate
request.recognitionLanguages = ["zh-Hans", "en-US"]
request.usesLanguageCorrection = true

let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
do {
    try handler.perform([request])
} catch {
    print("ERROR: \(error)")
    exit(1)
}

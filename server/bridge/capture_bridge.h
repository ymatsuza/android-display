#ifndef CAPTURE_BRIDGE_H
#define CAPTURE_BRIDGE_H

#include <CoreGraphics/CoreGraphics.h>
#include <CoreVideo/CoreVideo.h>

typedef void (*FrameCallback)(void *pixelBuffer, int width, int height, int64_t timestamp, void *userData);

typedef struct {
    void *stream;
    int success;
    char errorMsg[256];
} CaptureResult;

CaptureResult StartCapture(CGDirectDisplayID displayID, int fps, FrameCallback callback, void *userData);
void StopCapture(void *stream);
void ReleasePixelBuffer(void *pixelBuffer);

#endif

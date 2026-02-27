#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <IOSurface/IOSurface.h>
#include "capture_bridge.h"
#include <string.h>
#include <mach/mach_time.h>
#include <unistd.h>

// CaptureContext bundles the callback, user-data, and the display stream
// reference so the block-based handler can forward frames to Go.
typedef struct {
    FrameCallback callback;
    void *userData;
    CGDisplayStreamRef stream;
} CaptureContext;

CaptureResult StartCapture(CGDirectDisplayID displayID, int fps, FrameCallback callback, void *userData) {
    CaptureResult result;
    memset(&result, 0, sizeof(result));

    // Query actual display size from CoreGraphics (works for virtual displays).
    CGRect bounds = CGDisplayBounds(displayID);
    size_t width  = (size_t)bounds.size.width;
    size_t height = (size_t)bounds.size.height;

    if (width == 0 || height == 0) {
        snprintf(result.errorMsg, sizeof(result.errorMsg),
                 "display %u has zero size (%.0fx%.0f)", displayID,
                 bounds.size.width, bounds.size.height);
        return result;
    }

    // Configure the stream: minimum frame interval, show cursor, BGRA pixel format.
    double interval = 1.0 / (double)fps;
    NSDictionary *properties = @{
        (__bridge NSString *)kCGDisplayStreamMinimumFrameTime : @(interval),
        (__bridge NSString *)kCGDisplayStreamShowCursor       : @YES,
        (__bridge NSString *)kCGDisplayStreamQueueDepth       : @3,
    };

    // Allocate context on the heap so the block can reference it.
    CaptureContext *ctx = (CaptureContext *)calloc(1, sizeof(CaptureContext));
    if (!ctx) {
        snprintf(result.errorMsg, sizeof(result.errorMsg), "failed to allocate context");
        return result;
    }
    ctx->callback = callback;
    ctx->userData = userData;

    // Dedicated serial queue at user-interactive QoS.
    dispatch_queue_attr_t attr = dispatch_queue_attr_make_with_qos_class(
        DISPATCH_QUEUE_SERIAL, QOS_CLASS_USER_INTERACTIVE, 0);
    dispatch_queue_t captureQueue = dispatch_queue_create(
        "com.androidmac.capture", attr);

    // Create the display stream using CGDisplayStream.
    // Unlike ScreenCaptureKit, this takes a CGDirectDisplayID directly —
    // no "shareable content" discovery step, so virtual displays are always found.
    CGDisplayStreamRef stream = CGDisplayStreamCreateWithDispatchQueue(
        displayID,
        width,
        height,
        kCVPixelFormatType_32BGRA,
        (__bridge CFDictionaryRef)properties,
        captureQueue,
        ^(CGDisplayStreamFrameStatus status,
          uint64_t displayTime,
          IOSurfaceRef frameSurface,
          CGDisplayStreamUpdateRef updateRef)
    {
        if (status != kCGDisplayStreamFrameStatusFrameComplete || !frameSurface) {
            return;
        }

        // Wrap IOSurface in a CVPixelBuffer for the encoder.
        CVPixelBufferRef pixelBuffer = NULL;
        CVReturn cvErr = CVPixelBufferCreateWithIOSurface(
            kCFAllocatorDefault, frameSurface, NULL, &pixelBuffer);
        if (cvErr != kCVReturnSuccess || !pixelBuffer) {
            return;
        }

        int w = (int)CVPixelBufferGetWidth(pixelBuffer);
        int h = (int)CVPixelBufferGetHeight(pixelBuffer);

        // Timestamp: convert Mach absolute time → microseconds.
        // displayTime is in Mach absolute units; convert via timebase.
        mach_timebase_info_data_t tb;
        mach_timebase_info(&tb);
        int64_t timestamp = (int64_t)((double)displayTime * tb.numer / tb.denom / 1000.0);

        if (ctx->callback) {
            ctx->callback((void *)pixelBuffer, w, h, timestamp, ctx->userData);
        } else {
            CVPixelBufferRelease(pixelBuffer);
        }
    });

    if (!stream) {
        free(ctx);
        snprintf(result.errorMsg, sizeof(result.errorMsg),
                 "CGDisplayStreamCreate failed for display %u (%zux%zu)",
                 displayID, width, height);
        return result;
    }

    // Start the stream.
    CGError startErr = CGDisplayStreamStart(stream);
    if (startErr != kCGErrorSuccess) {
        CFRelease(stream);
        free(ctx);
        snprintf(result.errorMsg, sizeof(result.errorMsg),
                 "CGDisplayStreamStart failed: %d", (int)startErr);
        return result;
    }

    ctx->stream = stream;
    result.stream  = (void *)ctx;   // ownership passes to caller
    result.success = 1;
    return result;
}

void StopCapture(void *streamPtr) {
    if (!streamPtr) return;

    CaptureContext *ctx = (CaptureContext *)streamPtr;
    if (ctx->stream) {
        CGDisplayStreamStop(ctx->stream);
        // Give the stop a moment to flush pending callbacks.
        usleep(100000); // 100 ms
        CFRelease(ctx->stream);
        ctx->stream = NULL;
    }
    free(ctx);
}

void ReleasePixelBuffer(void *pixelBuffer) {
    if (pixelBuffer != NULL) {
        CVPixelBufferRelease((CVPixelBufferRef)pixelBuffer);
    }
}
